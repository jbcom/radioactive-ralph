package fixit

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// RunOptions drives the full six-stage pipeline.
type RunOptions struct {
	RepoRoot       string
	Topic          string
	Description    string
	Constraints    []string
	NonInteractive bool

	// ProviderBinding selects the provider used for stage-4 planning.
	ProviderBinding provider.Binding

	// ProviderBinary overrides the planning provider binary. Tests may
	// point this at a fake CLI.
	ProviderBinary string

	// RunnerFactory constructs the planning provider runner. Nil uses
	// provider.NewRunner.
	RunnerFactory func(binding provider.Binding) (provider.Runner, error)

	// SkipAnalysis bypasses Stage 4 — useful only for tests that
	// exercise the deterministic stages without spawning a
	// subprocess. When true, the pipeline returns after Stage 3 with
	// a zero PlanProposal.
	SkipAnalysis bool

	// MaxRefinementIterations caps how many rounds of provider-backed
	// refinement Stage 4 will do before giving up. Default 3.
	// Configurable via CLI flag or config.toml [variants.fixit]
	// max_refinement_iterations.
	MaxRefinementIterations int

	// MinConfidenceThreshold is the confidence floor a proposal must
	// meet before we stop refining. Validate.MinConfidence is the
	// lower absolute floor below which validation fails; this
	// threshold is the refinement-loop bar. Default 70.
	// Configurable via CLI flag or config.toml [variants.fixit]
	// min_confidence_threshold.
	MinConfidenceThreshold int

	// PlanModel pins the planning tier for Stage 4. Empty defaults to
	// "opus". Configurable via CLI or config.toml [variants.fixit]
	// plan_model.
	PlanModel string

	// PlanEffort pins the reasoning-effort level for Stage 4. Empty
	// defaults to "high". Configurable via CLI or config.toml
	// [variants.fixit] plan_effort.
	PlanEffort string
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

	// Stage 4 + Stage 5 run as a refinement loop: Analyze→Validate,
	// feed failures back into Analyze, repeat until passing or capped.
	refined, err := Refine(ctx, rc, intent, RefineOptions{
		AnalyzeOpts: AnalyzeOptions{
			Intent:         intent,
			RC:             rc,
			Scores:         scores,
			Binding:        opts.ProviderBinding,
			ProviderBinary: opts.ProviderBinary,
			RunnerFactory:  opts.RunnerFactory,
			WorkingDir:     opts.RepoRoot,
			Model:          opts.PlanModel,
			Effort:         opts.PlanEffort,
		},
		MaxIterations:          opts.MaxRefinementIterations,
		MinConfidenceThreshold: opts.MinConfidenceThreshold,
	})
	if err != nil {
		return EmitFallback(plansDir, intent.Topic,
			"Stage 4 refinement loop failed: "+err.Error(), "",
			intent, rc)
	}

	// Status: current iff the loop accepted; provisional otherwise
	// (we still emit the best attempt so the operator can see it).
	status := StatusProvisional
	if refined.AcceptedAt > 0 {
		status = StatusCurrent
	}

	// Emit with the final proposal + its final validation.
	emitted, err := Emit(plansDir, intent.Topic, refined.FinalProposal,
		refined.FinalValidation, status, intent, rc)
	if err != nil {
		return EmittedPlan{}, err
	}

	// Annotate the emitted plan with the refinement history so the
	// operator can see how many passes it took and what each pass
	// fixed. The annotation is appended to the file — Stage 6's
	// emitter writes a canonical report body; refinement history is
	// additional diagnostic information.
	if err := appendRefinementHistory(emitted.Path, refined); err != nil {
		// Non-fatal; log via the returned EmittedPlan rather than
		// blocking successful emission.
		_ = err
	}
	return emitted, nil
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
