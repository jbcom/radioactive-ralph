package fixit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
)

// EmitToDAGOpts configures EmitToDAG.
type EmitToDAGOpts struct {
	Store      *plandag.Store
	Topic      string
	Proposal   PlanProposal
	Validation ValidationResult
	Status     PlanStatus
	Intent     IntentSpec
	RC         RepoContext
	RawOutput  string // Phase 2 provider output; stored in analyses.raw_json
}

// EmitResult is what EmitToDAG returns.
type EmitResult struct {
	PlanID     string
	Status     PlanStatus
	TaskIDs    []string
	Proposal   PlanProposal
	Validation ValidationResult
}

// EmitToDAG writes the fixit output into plandag. Creates:
//   - one plans row (status mapped from fixit PlanStatus)
//   - one intents row capturing raw operator input
//   - one analyses row capturing Stage 4 provider output
//   - N tasks rows (one per proposal.Tasks entry)
//   - topologically-sorted dependency edges when Task.DependsOn is set
//
// Returns the newly-generated plan UUID plus the slugified task IDs.
func EmitToDAG(ctx context.Context, o EmitToDAGOpts) (EmitResult, error) {
	if o.Store == nil {
		return EmitResult{}, fmt.Errorf("fixit: EmitToDAG requires a plandag.Store")
	}
	if o.Topic == "" {
		return EmitResult{}, fmt.Errorf("fixit: EmitToDAG requires a topic slug")
	}

	planID, err := o.Store.CreatePlan(ctx, plandag.CreatePlanOpts{
		Slug:           o.Topic,
		Title:          "Fixit advisor — " + o.Topic,
		RepoPath:       o.RC.GitRoot,
		PrimaryVariant: o.Proposal.Primary,
		Confidence:     o.Proposal.Confidence,
	})
	if err != nil {
		return EmitResult{}, fmt.Errorf("fixit: CreatePlan: %w", err)
	}

	// Intents + analyses rows for audit trail. Constraints get
	// serialized as the sources blob since IntentSpec doesn't expose
	// an explicit Sources field yet.
	sourcesJSON, _ := json.Marshal(o.Intent.Constraints)
	if _, err := o.Store.DB().ExecContext(ctx, `
		INSERT INTO intents(plan_id, raw_input, sources_json)
		VALUES (?, ?, ?)
	`, planID, o.Intent.Description, string(sourcesJSON)); err != nil {
		return EmitResult{}, fmt.Errorf("fixit: insert intent: %w", err)
	}

	if o.RawOutput != "" {
		if _, err := o.Store.DB().ExecContext(ctx, `
			INSERT INTO analyses(plan_id, intent_id, model, effort, confidence, raw_json)
			VALUES (?, (SELECT MAX(id) FROM intents WHERE plan_id = ?), ?, ?, ?, ?)
		`, planID, planID, "opus", "high", o.Proposal.Confidence, o.RawOutput); err != nil {
			return EmitResult{}, fmt.Errorf("fixit: insert analysis: %w", err)
		}
	}

	// Tasks — first pass creates them all; second pass wires deps
	// so every referenced predecessor exists.
	taskIDs := make([]string, 0, len(o.Proposal.Tasks))
	titleToID := make(map[string]string, len(o.Proposal.Tasks))
	for i, t := range o.Proposal.Tasks {
		taskID := slugifyTaskID(t.Title, i)
		taskIDs = append(taskIDs, taskID)
		titleToID[t.Title] = taskID

		acceptance, _ := json.Marshal(t.AcceptanceCriteria)

		if err := o.Store.CreateTask(ctx, plandag.CreateTaskOpts{
			PlanID:          planID,
			ID:              taskID,
			Description:     t.Title,
			Complexity:      t.Impact,
			Effort:          t.Effort,
			VariantHint:     t.VariantHint,
			ContextBoundary: t.ContextBoundary,
			AcceptanceJSON:  string(acceptance),
		}); err != nil {
			return EmitResult{}, fmt.Errorf("fixit: create task %s: %w", taskID, err)
		}
	}

	for i, t := range o.Proposal.Tasks {
		for _, dep := range t.DependsOn {
			// DependsOn entries are task titles (or slugs); try both
			// — slug match first since fixit may emit either.
			depID := dep
			if mapped, ok := titleToID[dep]; ok {
				depID = mapped
			}
			if err := o.Store.AddDep(ctx, planID, taskIDs[i], depID); err != nil {
				return EmitResult{}, fmt.Errorf("fixit: add dep %s → %s: %w", taskIDs[i], depID, err)
			}
		}
	}

	// Wire plan-proposal-level acceptance criteria into the last task
	// so they participate in completion gating. Fixit's historic
	// shape put criteria at the top level; the DAG encodes them as
	// tasks for uniformity.
	if len(o.Proposal.AcceptanceCriteria) > 0 {
		critID := "acceptance-criteria"
		critBody := "Verify plan-level acceptance criteria:\n- " +
			strings.Join(o.Proposal.AcceptanceCriteria, "\n- ")
		acceptance, _ := json.Marshal(o.Proposal.AcceptanceCriteria)
		if err := o.Store.CreateTask(ctx, plandag.CreateTaskOpts{
			PlanID:         planID,
			ID:             critID,
			Description:    critBody,
			Complexity:     "S",
			Effort:         "S",
			VariantHint:    "blue", // read-only review — verify, don't mutate
			AcceptanceJSON: string(acceptance),
		}); err != nil {
			return EmitResult{}, fmt.Errorf("fixit: create acceptance task: %w", err)
		}
		// Every task feeds into the acceptance task so it runs last.
		for _, id := range taskIDs {
			if err := o.Store.AddDep(ctx, planID, critID, id); err != nil {
				return EmitResult{}, fmt.Errorf("fixit: add acceptance dep: %w", err)
			}
		}
		taskIDs = append(taskIDs, critID)
	}

	// Activate the plan once tasks are in place.
	if err := o.Store.SetPlanStatus(ctx, planID, mapFixitStatusToPlanDAG(o.Status)); err != nil {
		return EmitResult{}, err
	}

	return EmitResult{
		PlanID:     planID,
		Status:     o.Status,
		TaskIDs:    taskIDs,
		Proposal:   o.Proposal,
		Validation: o.Validation,
	}, nil
}

// mapFixitStatusToPlanDAG translates fixit's PlanStatus enum to
// plandag's. Only `current` maps to active; everything else is
// left in draft until an operator promotes it.
func mapFixitStatusToPlanDAG(s PlanStatus) plandag.PlanStatus {
	switch s {
	case StatusCurrent:
		return plandag.PlanStatusActive
	case StatusProvisional, StatusFallback:
		return plandag.PlanStatusDraft
	default:
		return plandag.PlanStatusDraft
	}
}

// slugifyTaskID produces a URL-safe task id from a human title.
// Falls back to "task-<index>" if the title reduces to empty.
func slugifyTaskID(title string, index int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	// Squash consecutive dashes that the above loop may produce.
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if out == "" {
		return fmt.Sprintf("task-%d", index+1)
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}
