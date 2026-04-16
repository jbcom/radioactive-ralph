package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	ralphxdg "github.com/jbcom/radioactive-ralph/internal/xdg"
)

// PlanCmd is `radioactive_ralph plan <sub>`.
// This is the durable plan-DAG surface — direct CRUD against the
// SQLite store under $XDG_STATE_HOME/radioactive_ralph/.
type PlanCmd struct {
	Ls        PlanLsCmd        `cmd:"" help:"List plans for this repo by default. Use --all-repos to widen the view."`
	Show      PlanShowCmd      `cmd:"" help:"Show one plan's tasks and current ready set."`
	Next      PlanNextCmd      `cmd:"" help:"Print the next ready task for a plan (without claiming it)."`
	Tasks     PlanTasksCmd     `cmd:"" help:"List tasks for one plan."`
	Approvals PlanApprovalsCmd `cmd:"" help:"List tasks in this repo waiting for operator approval."`
	Blocked   PlanBlockedCmd   `cmd:"" help:"List tasks in this repo that are blocked or waiting on more context."`
	Approve   PlanApproveCmd   `cmd:"" help:"Approve a task and return it to the runnable queue."`
	Requeue   PlanRequeueCmd   `cmd:"" help:"Requeue a blocked/failed task back into the runnable queue."`
	Handoff   PlanHandoffCmd   `cmd:"" help:"Hand a blocked/failed task to a different variant."`
	Retry     PlanRetryCmd     `cmd:"" help:"Retry a blocked or failed task."`
	Fail      PlanFailCmd      `cmd:"" help:"Force-fail a task from the operator surface."`
	History   PlanHistoryCmd   `cmd:"" help:"Show recent task events for one task."`

	// Import seeds a plan from a JSON file. Used during the
	// dogfooding bootstrap before fixit owns full durable planning.
	Import PlanImportCmd `cmd:"" help:"Import tasks into a new plan from a JSON file."`

	// MarkDone lets an operator (or shell script acting as one)
	// close out a task manually while we bootstrap.
	MarkDone PlanMarkDoneCmd `cmd:"mark-done" help:"Mark a running task as done."`
}

// PlanLsCmd implements `plan ls`.
type PlanLsCmd struct {
	All      bool `help:"Include archived + abandoned plans."`
	AllRepos bool `help:"Include plans from every repo in the operator state dir."`
}

func (c *PlanLsCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	var statuses []plandag.PlanStatus
	if c.All {
		statuses = []plandag.PlanStatus{
			plandag.PlanStatusDraft, plandag.PlanStatusActive,
			plandag.PlanStatusPaused, plandag.PlanStatusDone,
			plandag.PlanStatusFailedPartial, plandag.PlanStatusArchived,
			plandag.PlanStatusAbandoned,
		}
	}

	plans, err := store.ListPlans(rc.ctx, statuses)
	if err != nil {
		return err
	}
	repo := ""
	if !c.AllRepos {
		repo, err = resolveRepoRoot("")
		if err != nil {
			return err
		}
	}
	if len(plans) == 0 {
		fmt.Println("no plans in this state dir (yet)")
		return nil
	}
	found := false
	for _, p := range plans {
		if repo != "" && p.RepoPath != repo {
			continue
		}
		found = true
		fmt.Printf("%s  %-20s  %-12s  %s\n",
			p.ID, trunc(p.Slug, 20), p.Status, trunc(p.Title, 60))
	}
	if !found {
		fmt.Println("no plans for this repo (yet)")
	}
	return nil
}

// PlanShowCmd implements `plan show <id-or-slug>`.
type PlanShowCmd struct {
	IDOrSlug string `arg:"" help:"Plan UUID or slug."`
}

// PlanTasksCmd implements `plan tasks <id-or-slug>`.
type PlanTasksCmd struct {
	IDOrSlug string   `arg:"" help:"Plan UUID or slug."`
	Status   []string `help:"Only include these task statuses (repeatable)."`
}

func (c *PlanTasksCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plan, err := resolvePlan(rc.ctx, store, c.IDOrSlug, repo)
	if err != nil {
		return err
	}
	statuses, err := parseTaskStatuses(c.Status)
	if err != nil {
		return err
	}
	tasks, err := store.ListTasks(rc.ctx, plan.ID, statuses)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Printf("no matching tasks for plan %s\n", plan.Slug)
		return nil
	}
	for _, task := range tasks {
		line := fmt.Sprintf("[%-22s] %s  %s", task.Status, task.ID, task.Description)
		var extras []string
		if task.VariantHint != "" {
			extras = append(extras, "hint="+task.VariantHint)
		}
		if task.AssignedVariant != "" {
			extras = append(extras, "assigned="+task.AssignedVariant)
		}
		if task.ClaimedBySession != "" {
			extras = append(extras, "session="+task.ClaimedBySession)
		}
		if len(extras) > 0 {
			line += "  (" + strings.Join(extras, ", ") + ")"
		}
		fmt.Println(line)
	}
	return nil
}

// PlanApprovalsCmd implements `plan approvals`.
type PlanApprovalsCmd struct{}

func (c *PlanApprovalsCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plans, err := store.ListPlans(rc.ctx, []plandag.PlanStatus{
		plandag.PlanStatusActive,
		plandag.PlanStatusPaused,
	})
	if err != nil {
		return err
	}

	found := false
	for _, plan := range plans {
		if plan.RepoPath != repo {
			continue
		}
		tasks, err := store.ListTasks(rc.ctx, plan.ID, []plandag.TaskStatus{plandag.TaskStatusReadyPendingApproval})
		if err != nil {
			return err
		}
		for _, task := range tasks {
			found = true
			reason := latestTaskReason(rc.ctx, store, plan.ID, task.ID)
			fmt.Printf("%s  %s  %s", trunc(plan.Slug, 20), task.ID, task.Description)
			var extras []string
			if task.VariantHint != "" {
				extras = append(extras, "hint="+task.VariantHint)
			}
			if reason != "" {
				extras = append(extras, "reason="+reason)
			}
			if len(extras) > 0 {
				fmt.Printf("  (%s)", strings.Join(extras, ", "))
			}
			fmt.Println()
		}
	}
	if !found {
		fmt.Println("no tasks are waiting for operator approval in this repo")
	}
	return nil
}

// PlanBlockedCmd implements `plan blocked`.
type PlanBlockedCmd struct{}

func (c *PlanBlockedCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	items, err := store.ListRepoTaskSummaries(rc.ctx, repo, []plandag.TaskStatus{plandag.TaskStatusBlocked}, 50)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no blocked tasks in this repo")
		return nil
	}
	for _, item := range items {
		payload, _ := plandag.ParseTaskPayload(item.LatestPayloadJSON)
		fmt.Printf("%s  %s  %s", trunc(item.PlanSlug, 20), item.Task.ID, item.Task.Description)
		var extras []string
		if payload.Reason != "" {
			extras = append(extras, "reason="+payload.Reason)
		}
		if len(payload.NeedsContext) > 0 {
			extras = append(extras, "needs_context="+strings.Join(payload.NeedsContext, "|"))
		}
		if payload.HandoffTo != "" {
			extras = append(extras, "handoff_to="+payload.HandoffTo)
		}
		if len(extras) > 0 {
			fmt.Printf("  (%s)", strings.Join(extras, ", "))
		}
		fmt.Println()
	}
	return nil
}

// PlanApproveCmd implements `plan approve <plan> <task>`.
type PlanApproveCmd struct {
	PlanIDOrSlug string `arg:"" help:"Plan UUID or slug."`
	TaskID       string `arg:"" help:"Task id within the plan."`
}

func (c *PlanApproveCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}
	if err := store.ApproveTask(rc.ctx, plan.ID, c.TaskID); err != nil {
		return err
	}
	fmt.Printf("approved %s in plan %s\n", c.TaskID, plan.Slug)
	return nil
}

// PlanRequeueCmd implements `plan requeue <plan> <task>`.
type PlanRequeueCmd struct {
	PlanIDOrSlug    string `arg:"" help:"Plan UUID or slug."`
	TaskID          string `arg:"" help:"Task id within the plan."`
	Reason          string `help:"Operator reason for requeueing the task."`
	VariantHint     string `help:"Optional new variant hint for the requeued task."`
	RequireApproval bool   `help:"Return the task to approval-gated state instead of runnable pending."`
}

func (c *PlanRequeueCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}
	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}
	if err := store.OperatorRequeueTask(rc.ctx, plan.ID, c.TaskID, plandag.TaskEventPayload{
		Reason:         c.Reason,
		HandoffTo:      c.VariantHint,
		OperatorAction: "requeue",
	}, c.VariantHint, c.RequireApproval); err != nil {
		return err
	}
	fmt.Printf("requeued %s in plan %s\n", c.TaskID, plan.Slug)
	return nil
}

// PlanHandoffCmd implements `plan handoff <plan> <task> <variant>`.
type PlanHandoffCmd struct {
	PlanIDOrSlug    string `arg:"" help:"Plan UUID or slug."`
	TaskID          string `arg:"" help:"Task id within the plan."`
	Variant         string `arg:"" help:"Variant to hint for the next run."`
	Reason          string `help:"Operator reason for the handoff."`
	RequireApproval bool   `help:"Keep the task approval-gated after the handoff."`
}

func (c *PlanHandoffCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}
	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}
	if err := store.OperatorHandoffTask(rc.ctx, plan.ID, c.TaskID, plandag.TaskEventPayload{
		Reason:         c.Reason,
		HandoffTo:      c.Variant,
		OperatorAction: "handoff",
	}, c.Variant, c.RequireApproval); err != nil {
		return err
	}
	fmt.Printf("handed off %s in plan %s to %s\n", c.TaskID, plan.Slug, c.Variant)
	return nil
}

// PlanRetryCmd implements `plan retry <plan> <task>`.
type PlanRetryCmd struct {
	PlanIDOrSlug string `arg:"" help:"Plan UUID or slug."`
	TaskID       string `arg:"" help:"Task id within the plan."`
	Reason       string `help:"Operator reason for retrying the task."`
}

func (c *PlanRetryCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}
	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}
	if err := store.OperatorRetryTask(rc.ctx, plan.ID, c.TaskID, plandag.TaskEventPayload{
		Reason:         c.Reason,
		OperatorAction: "retry",
		Retryable:      true,
	}); err != nil {
		return err
	}
	fmt.Printf("retry requested for %s in plan %s\n", c.TaskID, plan.Slug)
	return nil
}

// PlanFailCmd implements `plan fail <plan> <task>`.
type PlanFailCmd struct {
	PlanIDOrSlug string `arg:"" help:"Plan UUID or slug."`
	TaskID       string `arg:"" help:"Task id within the plan."`
	Reason       string `help:"Operator reason for force-failing the task."`
}

func (c *PlanFailCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}
	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}
	if err := store.OperatorFailTask(rc.ctx, plan.ID, c.TaskID, plandag.TaskEventPayload{
		Reason:         c.Reason,
		OperatorAction: "fail",
	}); err != nil {
		return err
	}
	fmt.Printf("force-failed %s in plan %s\n", c.TaskID, plan.Slug)
	return nil
}

// PlanHistoryCmd implements `plan history <plan> <task>`.
type PlanHistoryCmd struct {
	PlanIDOrSlug string `arg:"" help:"Plan UUID or slug."`
	TaskID       string `arg:"" help:"Task id within the plan."`
	Limit        int    `help:"Maximum number of events to show." default:"20"`
}

func (c *PlanHistoryCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}
	events, err := store.ListTaskEvents(rc.ctx, plan.ID, c.TaskID, c.Limit)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Printf("no events for %s in plan %s\n", c.TaskID, plan.Slug)
		return nil
	}
	for _, event := range events {
		ts := event.OccurredAt.Format(time.RFC3339)
		if event.OccurredAt.IsZero() {
			ts = "unknown-time"
		}
		line := fmt.Sprintf("%s  %-18s", ts, event.EventType)
		var extras []string
		if event.Variant != "" {
			extras = append(extras, "variant="+event.Variant)
		}
		if event.SessionID != "" {
			extras = append(extras, "session="+event.SessionID)
		}
		payload := strings.TrimSpace(event.PayloadJSON)
		if payload != "" && payload != "{}" {
			extras = append(extras, "payload="+payload)
		}
		if len(extras) > 0 {
			line += "  " + strings.Join(extras, "  ")
		}
		fmt.Println(line)
	}
	return nil
}

func (c *PlanShowCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plan, err := resolvePlan(rc.ctx, store, c.IDOrSlug, repo)
	if err != nil {
		return err
	}
	fmt.Printf("Plan %s\n  slug:           %s\n  title:          %s\n  status:         %s\n  primary_variant: %s\n  confidence:     %d\n\n",
		plan.ID, plan.Slug, plan.Title, plan.Status, plan.PrimaryVariant, plan.Confidence)

	ready, err := store.Ready(rc.ctx, plan.ID)
	if err != nil {
		return err
	}
	fmt.Printf("Ready now (%d):\n", len(ready))
	for _, t := range ready {
		fmt.Printf("  [%-12s] %s  %s\n", t.Status, t.ID, t.Description)
	}
	return nil
}

// PlanNextCmd implements `plan next <id-or-slug>`.
type PlanNextCmd struct {
	IDOrSlug string `arg:"" help:"Plan UUID or slug."`
	JSON     bool   `help:"Emit the next task as JSON instead of human-readable."`
}

func (c *PlanNextCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plan, err := resolvePlan(rc.ctx, store, c.IDOrSlug, repo)
	if err != nil {
		return err
	}
	ready, err := store.Ready(rc.ctx, plan.ID)
	if err != nil {
		return err
	}
	if len(ready) == 0 {
		fmt.Fprintln(os.Stderr, "no ready task")
		return fmt.Errorf("no ready task")
	}
	next := ready[0]
	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"id":           next.ID,
			"description":  next.Description,
			"complexity":   next.Complexity,
			"effort":       next.Effort,
			"variant_hint": next.VariantHint,
		})
	}
	fmt.Printf("next: %s\n  %s\n", next.ID, next.Description)
	if next.VariantHint != "" {
		fmt.Printf("  hint: variant=%s  complexity=%s  effort=%s\n",
			next.VariantHint, next.Complexity, next.Effort)
	}
	return nil
}

// PlanImportCmd implements `plan import <json-file>`.
type PlanImportCmd struct {
	Path string `arg:"" help:"Path to JSON file."`
}

// importFile is the on-disk shape of plan import JSON.
type importFile struct {
	Slug            string       `json:"slug"`
	Title           string       `json:"title"`
	PrimaryVariant  string       `json:"primary_variant"`
	Confidence      int          `json:"confidence"`
	Intent          string       `json:"intent,omitempty"`
	Tasks           []importTask `json:"tasks"`
	ParallelismSets [][]string   `json:"parallelism_sets,omitempty"`
}

type importTask struct {
	ID              string   `json:"id"`
	Description     string   `json:"description"`
	Complexity      string   `json:"complexity,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	VariantHint     string   `json:"variant_hint,omitempty"`
	ContextBoundary bool     `json:"context_boundary,omitempty"`
	Acceptance      []string `json:"acceptance,omitempty"`
	DependsOn       []string `json:"depends_on,omitempty"`
}

func (c *PlanImportCmd) Run(rc *runContext) error {
	raw, err := os.ReadFile(c.Path) //nolint:gosec // operator-supplied path
	if err != nil {
		return fmt.Errorf("read %s: %w", c.Path, err)
	}
	var f importFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse %s: %w", c.Path, err)
	}
	if f.Slug == "" || f.Title == "" || len(f.Tasks) == 0 {
		return fmt.Errorf("plan import: slug, title, and tasks[] required")
	}

	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// Try to resolve repo path from cwd for indexing.
	repoPath, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	planID, err := store.CreatePlan(rc.ctx, plandag.CreatePlanOpts{
		Slug:           f.Slug,
		Title:          f.Title,
		RepoPath:       repoPath,
		PrimaryVariant: f.PrimaryVariant,
		Confidence:     f.Confidence,
	})
	if err != nil {
		return err
	}

	for _, t := range f.Tasks {
		acceptance, _ := json.Marshal(t.Acceptance)
		if err := store.CreateTask(rc.ctx, plandag.CreateTaskOpts{
			PlanID:          planID,
			ID:              t.ID,
			Description:     t.Description,
			Complexity:      t.Complexity,
			Effort:          t.Effort,
			VariantHint:     t.VariantHint,
			ContextBoundary: t.ContextBoundary,
			AcceptanceJSON:  string(acceptance),
		}); err != nil {
			return fmt.Errorf("create task %s: %w", t.ID, err)
		}
	}
	// Deps come second so every task exists first.
	for _, t := range f.Tasks {
		for _, dep := range t.DependsOn {
			if err := store.AddDep(rc.ctx, planID, t.ID, dep); err != nil {
				return fmt.Errorf("add dep %s→%s: %w", t.ID, dep, err)
			}
		}
	}

	if err := store.SetPlanStatus(rc.ctx, planID, plandag.PlanStatusActive); err != nil {
		return err
	}

	fmt.Printf("imported plan %s (%d task(s))\n  id: %s\n",
		f.Slug, len(f.Tasks), planID)
	return nil
}

// PlanMarkDoneCmd implements `plan mark-done <plan> <task>`.
type PlanMarkDoneCmd struct {
	PlanIDOrSlug string `arg:"" help:"Plan UUID or slug."`
	TaskID       string `arg:"" help:"Task id within the plan."`
	Evidence     string `help:"Evidence payload (JSON or short string)."`
}

func (c *PlanMarkDoneCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	repo, err := resolveRepoRoot("")
	if err != nil {
		return err
	}

	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
	if err != nil {
		return err
	}

	// Claim-then-done path so the FK is satisfied even if the
	// task is still 'pending' (operators using this during bootstrap
	// may skip the running transition).
	task, err := store.GetTask(rc.ctx, plan.ID, c.TaskID)
	if err != nil {
		return err
	}
	if task.Status == plandag.TaskStatusPending {
		// Self-attribute: create a one-off operator session so the FK
		// lands somewhere real. Expires on process exit; future
		// reaper sweeps clean it up.
		sessID, err := store.CreateSession(rc.ctx, plandag.SessionOpts{
			Mode:         plandag.SessionModeAttached,
			Transport:    plandag.SessionTransportStdio,
			PID:          os.Getpid(),
			PIDStartTime: "operator",
			Host:         "local",
		})
		if err != nil {
			return err
		}
		svID, err := store.CreateSessionVariant(rc.ctx, plandag.SessionVariantOpts{
			SessionID:           sessID,
			VariantName:         "operator",
			SubprocessPID:       os.Getpid(),
			SubprocessStartTime: "operator",
		})
		if err != nil {
			return err
		}
		if _, err := store.ClaimNextReady(rc.ctx, plan.ID, "operator", sessID, svID); err != nil {
			return fmt.Errorf("self-claim: %w", err)
		}
		if _, err := store.MarkDone(rc.ctx, plan.ID, c.TaskID, sessID, c.Evidence); err != nil {
			return err
		}
		fmt.Printf("marked done (operator): %s\n", c.TaskID)
		return nil
	}

	// Happy path: task is already running via a real session.
	if _, err := store.MarkDone(rc.ctx, plan.ID, c.TaskID, task.ClaimedBySession, c.Evidence); err != nil {
		return err
	}
	fmt.Printf("marked done: %s\n", c.TaskID)
	return nil
}

// openPlanStore resolves the per-operator plandag DSN under the
// Ralph state root (honors $RALPH_STATE_DIR for test isolation) and
// opens the store. Falls back to adrg/xdg if the internal resolver
// fails for any reason.
func openPlanStore(ctx context.Context) (*plandag.Store, error) {
	root, err := ralphxdg.StateRoot()
	if err != nil {
		// Last-resort fallback to adrg's XDG resolution.
		root, err = xdg.StateFile("radioactive_ralph")
		if err != nil {
			return nil, fmt.Errorf("xdg state: %w", err)
		}
	}
	stateDir := filepath.Join(root, "plans.db")
	if err := os.MkdirAll(filepath.Dir(stateDir), 0o755); err != nil { //nolint:gosec // operator state dir
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(stateDir), err)
	}
	dsn := "file:" + stateDir +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	return plandag.Open(ctx, plandag.Options{DSN: dsn})
}

// resolvePlan accepts either a full UUID or a slug and returns the
// matching plan for the current repo.
func resolvePlan(ctx context.Context, store *plandag.Store, ref string, repo string) (*plandag.Plan, error) {
	// Try by id first (UUID v7 looks like 36 chars with hyphens).
	if len(ref) == 36 && strings.Count(ref, "-") == 4 {
		plan, err := store.GetPlan(ctx, ref)
		if err != nil {
			return nil, err
		}
		if repo != "" && plan.RepoPath != repo {
			return nil, fmt.Errorf("plan %q belongs to a different repo", ref)
		}
		return plan, nil
	}
	// Otherwise treat as slug; scan all plans for the match.
	plans, err := store.ListPlans(ctx, []plandag.PlanStatus{
		plandag.PlanStatusDraft, plandag.PlanStatusActive,
		plandag.PlanStatusPaused, plandag.PlanStatusDone,
		plandag.PlanStatusFailedPartial,
	})
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		if p.Slug == ref && p.RepoPath == repo {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("no plan matching %q for this repo", ref)
}

func parseTaskStatuses(values []string) ([]plandag.TaskStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	allowed := map[string]plandag.TaskStatus{
		string(plandag.TaskStatusPending):              plandag.TaskStatusPending,
		string(plandag.TaskStatusReady):                plandag.TaskStatusReady,
		string(plandag.TaskStatusReadyPendingApproval): plandag.TaskStatusReadyPendingApproval,
		string(plandag.TaskStatusBlocked):              plandag.TaskStatusBlocked,
		string(plandag.TaskStatusRunning):              plandag.TaskStatusRunning,
		string(plandag.TaskStatusDone):                 plandag.TaskStatusDone,
		string(plandag.TaskStatusFailed):               plandag.TaskStatusFailed,
		string(plandag.TaskStatusSkipped):              plandag.TaskStatusSkipped,
		string(plandag.TaskStatusDecomposed):           plandag.TaskStatusDecomposed,
	}
	statuses := make([]plandag.TaskStatus, 0, len(values))
	for _, value := range values {
		status, ok := allowed[value]
		if !ok {
			return nil, fmt.Errorf("unknown task status %q", value)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func latestTaskReason(ctx context.Context, store *plandag.Store, planID, taskID string) string {
	events, err := store.ListTaskEvents(ctx, planID, taskID, 1)
	if err != nil || len(events) == 0 {
		return ""
	}
	payload := strings.TrimSpace(events[0].PayloadJSON)
	if payload == "" || payload == "{}" {
		return ""
	}
	parsed, err := plandag.ParseTaskPayload(payload)
	if err == nil && parsed.Reason != "" {
		return parsed.Reason
	}
	return payload
}

// trunc cuts s to n chars with an ellipsis.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
