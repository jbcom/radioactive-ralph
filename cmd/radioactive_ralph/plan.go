package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	ralphxdg "github.com/jbcom/radioactive-ralph/internal/xdg"
)

// PlanCmd is `radioactive_ralph plan <sub>`.
// This is the durable plan-DAG surface — direct CRUD against the
// SQLite store under $XDG_STATE_HOME/radioactive_ralph/.
type PlanCmd struct {
	Ls   PlanLsCmd   `cmd:"" help:"List plans in this operator's state dir."`
	Show PlanShowCmd `cmd:"" help:"Show one plan's tasks and current ready set."`
	Next PlanNextCmd `cmd:"" help:"Print the next ready task for a plan (without claiming it)."`

	// Import seeds a plan from a JSON file. Used during the
	// dogfooding bootstrap before the MCP server + fixit rewire.
	Import PlanImportCmd `cmd:"" help:"Import tasks into a new plan from a JSON file."`

	// MarkDone lets an operator (or shell script acting as one)
	// close out a task manually while we bootstrap.
	MarkDone PlanMarkDoneCmd `cmd:"mark-done" help:"Mark a running task as done."`
}

// PlanLsCmd implements `plan ls`.
type PlanLsCmd struct {
	All bool `help:"Include archived + abandoned plans."`
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
	if len(plans) == 0 {
		fmt.Println("no plans in this state dir (yet)")
		return nil
	}
	for _, p := range plans {
		fmt.Printf("%s  %-20s  %-12s  %s\n",
			p.ID, trunc(p.Slug, 20), p.Status, trunc(p.Title, 60))
	}
	return nil
}

// PlanShowCmd implements `plan show <id-or-slug>`.
type PlanShowCmd struct {
	IDOrSlug string `arg:"" help:"Plan UUID or slug."`
}

func (c *PlanShowCmd) Run(rc *runContext) error {
	store, err := openPlanStore(rc.ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	plan, err := resolvePlan(rc.ctx, store, c.IDOrSlug)
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

	plan, err := resolvePlan(rc.ctx, store, c.IDOrSlug)
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
	repoPath, _ := os.Getwd()

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

	plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug)
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
			Mode:         plandag.SessionModePortable,
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
// matching plan.
func resolvePlan(ctx context.Context, store *plandag.Store, ref string) (*plandag.Plan, error) {
	// Try by id first (UUID v7 looks like 36 chars with hyphens).
	if len(ref) == 36 && strings.Count(ref, "-") == 4 {
		return store.GetPlan(ctx, ref)
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
		if p.Slug == ref {
			return &p, nil
		}
	}
	return nil, fmt.Errorf("no plan matching %q", ref)
}

// trunc cuts s to n chars with an ellipsis.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
