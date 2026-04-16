package fixit

import (
	"context"
	"fmt"
	"strings"
)

// RefineOptions drives the Stage 4 + Stage 5 refinement loop.
type RefineOptions struct {
	AnalyzeOpts            AnalyzeOptions
	MaxIterations          int  // default 3
	MinConfidenceThreshold int  // default 70
	StopOnValidationPass   bool // default true — stop as soon as validation passes
}

// RefineResult captures each iteration the loop produced, plus the
// final accepted proposal (or zero value if all iterations failed).
type RefineResult struct {
	Iterations      int          // how many passes we actually ran
	FinalProposal   PlanProposal // the accepted or last-attempt proposal
	FinalValidation ValidationResult
	History         []RefineIteration
	AcceptedAt      int // iteration number of the accepted proposal (1-indexed), 0 if never
}

// RefineIteration records what happened in one pass.
type RefineIteration struct {
	Iteration  int
	Proposal   PlanProposal
	Validation ValidationResult
	Confidence int
	Accepted   bool
}

// Refine runs Stage 4 repeatedly, feeding validation failures back
// into each subsequent call as corrective context. Stops when any
// of:
//   - Proposal validates AND confidence >= MinConfidenceThreshold
//   - MaxIterations reached
//   - Hard error (context cancel, provider execution failure)
//
// The LLM sees each prior attempt's failures appended to the system
// prompt, so it can deliberately address them rather than randomly
// redrafting.
func Refine(ctx context.Context, rc RepoContext, intent IntentSpec, opts RefineOptions) (RefineResult, error) {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 3
	}
	if opts.MinConfidenceThreshold <= 0 {
		opts.MinConfidenceThreshold = 70
	}
	if !opts.StopOnValidationPass {
		opts.StopOnValidationPass = true
	}

	var result RefineResult
	var previousFailures []string
	var previousProposal *PlanProposal

	for i := 1; i <= opts.MaxIterations; i++ {
		// Build the options for this iteration. On iterations >1, we
		// append the prior failures + proposal to the operator's
		// description so the renderer bakes them into the prompt.
		iterOpts := opts.AnalyzeOpts
		iterOpts.Intent = intent
		iterOpts.RC = rc

		if i > 1 && previousProposal != nil {
			iterOpts.Intent = withFeedback(intent, *previousProposal, previousFailures, i)
		}

		proposal, err := Analyze(ctx, iterOpts)
		if err != nil {
			return result, fmt.Errorf("iteration %d: %w", i, err)
		}

		validation := Validate(proposal, rc, intent)
		accepted := validation.Passed && proposal.Confidence >= opts.MinConfidenceThreshold

		result.History = append(result.History, RefineIteration{
			Iteration:  i,
			Proposal:   proposal,
			Validation: validation,
			Confidence: proposal.Confidence,
			Accepted:   accepted,
		})
		result.Iterations = i
		result.FinalProposal = proposal
		result.FinalValidation = validation

		if accepted {
			result.AcceptedAt = i
			return result, nil
		}

		previousProposal = &proposal
		previousFailures = append([]string(nil), validation.Failures...)
		if proposal.Confidence < opts.MinConfidenceThreshold {
			previousFailures = append(previousFailures,
				fmt.Sprintf("confidence %d is below the required threshold of %d — tighten the plan so confidence rises",
					proposal.Confidence, opts.MinConfidenceThreshold))
		}
	}

	// MaxIterations exhausted without acceptance. Return the best
	// (last) attempt; the caller decides whether to emit as
	// provisional or fallback based on FinalValidation.Passed.
	return result, nil
}

// withFeedback returns an IntentSpec with refinement feedback baked
// into the Description so the prompt renderer naturally includes it
// under "Operator intent" — no template changes required.
//
// The appended block tells the planning model exactly what the prior
// pass got wrong and what the threshold is. The retry is deliberate:
// address these specific failures, raise confidence.
func withFeedback(base IntentSpec, prior PlanProposal, failures []string, iter int) IntentSpec {
	out := base
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", base.Description)
	fmt.Fprintf(&b, "# Refinement pass %d\n\n", iter)
	fmt.Fprintln(&b, "A previous attempt produced a proposal that did not meet the acceptance bar. You are refining it.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "**Previous proposal (to refine):**\n\n")
	fmt.Fprintf(&b, "- primary: %s\n", prior.Primary)
	fmt.Fprintf(&b, "- primary_rationale: %s\n", prior.PrimaryRationale)
	if prior.Alternate != "" {
		fmt.Fprintf(&b, "- alternate: %s (%s)\n", prior.Alternate, prior.AlternateWhen)
	}
	fmt.Fprintf(&b, "- confidence: %d\n", prior.Confidence)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "**Specific issues to address:**")
	for _, f := range failures {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Produce a refined PlanProposal that addresses every issue above. Keep the same schema.")
	fmt.Fprintln(&b, "Specifically: make every acceptance criterion use a measurable verb (passes, exists, merges, matches, returns, equal, ≥, ≤, or a specific number). Replace any vague verb (improves, considers, addresses, helps). Ensure every referenced file path either exists or is paired with a creation verb (Create/Scaffold/Add/Generate/Draft).")
	fmt.Fprintln(&b, "Raise confidence by being more specific and by cutting any task you can't defend with a measurable criterion.")
	out.Description = b.String()
	return out
}
