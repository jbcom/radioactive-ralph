package fixit

import (
	"context"
	"fmt"
	"path/filepath"
)

// RunOptions drives the full six-stage pipeline.
type RunOptions struct {
	RepoRoot       string
	Topic          string
	Description    string
	Constraints    []string
	NonInteractive bool

	// ClaudeBin overrides the default `claude` binary for Stage 4.
	// Tests pass the cassette replayer or fake-claude here.
	ClaudeBin string

	// SkipAnalysis bypasses Stage 4 — useful only for tests that
	// exercise the deterministic stages without spawning a
	// subprocess. When true, the pipeline returns after Stage 3 with
	// a zero PlanProposal.
	SkipAnalysis bool
}

// RunPipeline orchestrates Stages 1-6 and returns what was emitted.
// Errors here indicate an unrecoverable pipeline failure (e.g. can't
// explore the repo); Stage 4/5 failures become fallback or
// provisional plans, not errors.
func RunPipeline(ctx context.Context, opts RunOptions) (EmittedPlan, error) {
	// Stage 1
	intent, err := CaptureIntent(IntentOptions{
		Topic:          opts.Topic,
		Description:    opts.Description,
		Constraints:    opts.Constraints,
		NonInteractive: opts.NonInteractive,
		RepoRoot:       opts.RepoRoot,
	})
	if err != nil {
		return EmittedPlan{}, fmt.Errorf("stage 1 (intent): %w", err)
	}

	// Stage 2
	rc, err := Explore(ctx, opts.RepoRoot)
	if err != nil {
		return EmittedPlan{}, fmt.Errorf("stage 2 (explore): %w", err)
	}

	// Stage 3
	scores := Score(rc, intent)

	plansDir := filepath.Join(opts.RepoRoot, ".radioactive-ralph", "plans")

	// SkipAnalysis path — bail early with a deterministic fallback
	// that uses the top-scored non-disqualified variant. Only used by
	// tests that don't want to spawn a subprocess.
	if opts.SkipAnalysis {
		p := topDeterministicPick(scores)
		validation := Validate(p, rc, intent)
		status := StatusCurrent
		if !validation.Passed {
			status = StatusProvisional
		}
		return Emit(plansDir, intent.Topic, p, validation, status, intent, rc)
	}

	// Stage 4
	proposal, err := Analyze(ctx, AnalyzeOptions{
		Intent:     intent,
		RC:         rc,
		Scores:     scores,
		ClaudeBin:  opts.ClaudeBin,
		WorkingDir: opts.RepoRoot,
	})
	if err != nil {
		return EmitFallback(plansDir, intent.Topic,
			"Stage 4 Claude analysis failed: "+err.Error(), "",
			intent, rc)
	}

	// Stage 5
	validation := Validate(proposal, rc, intent)
	status := StatusCurrent
	if !validation.Passed {
		status = StatusProvisional
	}

	// Stage 6
	return Emit(plansDir, intent.Topic, proposal, validation, status, intent, rc)
}

// topDeterministicPick returns a PlanProposal built from the
// rule-based top-scored variant. Used only in --skip-analysis paths
// (tests) so the pipeline returns something usable without a real
// subprocess call.
func topDeterministicPick(scores []VariantScore) PlanProposal {
	for _, s := range scores {
		if len(s.Disqualifying) > 0 {
			continue
		}
		reasons := "deterministic top pick"
		if len(s.Reasons) > 0 {
			reasons = s.Reasons[0]
		}
		return PlanProposal{
			Primary:            s.Variant,
			PrimaryRationale:   reasons,
			Tasks:              []Task{{Title: "Start with the chosen variant", Effort: "M", Impact: "M"}},
			AcceptanceCriteria: []string{"Variant exists in registry", "Operator has accepted the pick", "Plan file exists at the expected path"},
			Confidence:         75,
		}
	}
	return PlanProposal{
		Primary:          "blue",
		PrimaryRationale: "No variant passed disqualification — defaulting to blue read-only",
		Confidence:       30,
	}
}
