// Package genesis implements the planning-genesis flow described in
// docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md §11
// ("Planning genesis"): turning a vague prompt (or an already-drafted
// markdown doc) into a complete, validator-clean plan document.
//
// The spec's framing is deliberate: rather than building question-
// extraction machinery, a small team of agents JUXTAPOSE and CHALLENGE
// each other's read of the input until it converges on a plan that covers
// the work end-to-end. The refined markdown document IS the review
// surface -- headless mode emits it, TUI mode renders it for scroll/edit
// review (see review.go). Users may also skip planning entirely and run
// their input as-is.
//
// This file implements the orchestration SHAPE: Refine takes a pluggable
// Refiner so the flow is fully testable with fakes; a real multi-agent
// juxtaposition implementation (dispatching real provider.Runner agents
// against each other) is a Refiner value documented alongside
// NewProviderJuxtaposition, not baked into Refine itself.
package genesis

import (
	"bytes"
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

// Refiner produces a candidate plan markdown document from the current
// best draft. Refine calls it in a loop (see RefineOptions.MaxRounds):
// each round's output becomes the next round's input, so a Refiner is
// free to make small incremental improvements rather than needing to
// solve the whole problem in one call. Round 0's input is the caller's
// original prompt/doc verbatim.
//
// A real implementation dispatches two or more provider agents that
// challenge/expand round-over-round; see the (forthcoming, real-CLI-
// backed) juxtaposition Refiner. Tests use a fake func value directly --
// Refiner is a plain func type so no interface/mock boilerplate is
// needed to exercise Refine's convergence/validation logic.
type Refiner func(ctx context.Context, draft string) (next string, done bool, err error)

// RefineOptions configures Refine.
type RefineOptions struct {
	// MaxRounds bounds how many times Refiner is invoked before Refine
	// gives up waiting for convergence. Must be >= 1. Defaults to 6 when
	// zero.
	MaxRounds int
}

// ErrRefinementDidNotConverge is returned when Refiner never reports
// done=true within MaxRounds with output that parses into at least one
// step. plan.Parse itself is extremely permissive (goldmark parses
// essentially any text into SOME AST), so "valid" here means both parses
// cleanly AND yields a non-empty step universe (plan.Plan.StepIDs) --
// otherwise a refiner could "converge" on an empty or narrative-only
// document that plan.Parse happily accepts but that is not actually a
// plan. Advisory plan.Validate findings (ambiguous sections, etc.) do NOT
// block convergence; a plan that merely has ambiguities the author should
// tighten up is still a usable plan, per plan.Validate's own doc comment
// ("advisory... never blocks Parse from running").
var ErrRefinementDidNotConverge = fmt.Errorf("genesis: refinement did not converge to a valid plan within the round budget")

// Refine runs the agent-juxtaposition refinement loop against input (a
// vague prompt OR a full markdown doc) using refiner, and returns the
// final plan markdown once refiner reports done=true AND the result
// parses cleanly per plan.Parse (the spec's "converging on a final
// markdown plan (validated by internal/plan.Validate)" requirement).
//
// Refine itself never talks to a CLI/network -- refiner does. This keeps
// the orchestration shape (round loop, convergence check, parse-validate
// gate) testable with an in-memory fake while a real implementation plugs
// in a Refiner that dispatches provider.Runner agents.
func Refine(ctx context.Context, input string, refiner Refiner, opts RefineOptions) ([]byte, error) {
	if refiner == nil {
		return nil, fmt.Errorf("genesis: refiner required")
	}
	maxRounds := opts.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 6
	}

	draft := input
	for round := 0; round < maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("genesis: refine round %d: %w", round, err)
		}

		next, done, err := refiner(ctx, draft)
		if err != nil {
			return nil, fmt.Errorf("genesis: refiner round %d: %w", round, err)
		}
		draft = next

		if !done {
			continue
		}

		md := []byte(draft)
		parsed, err := plan.Parse(md)
		if err != nil || len(parsed.StepIDs()) == 0 {
			// The refiner claimed convergence but the result either
			// doesn't parse or parses into zero actual steps -- give it
			// another round rather than silently handing back unusable
			// markdown. This treats a "done" round the same as any other
			// round if its output doesn't stand up as a plan.
			continue
		}
		return md, nil
	}

	return nil, ErrRefinementDidNotConverge
}

// Skip implements the spec's "users may also skip planning and run their
// input as-is" path: it returns input unchanged as the plan document, with
// no refinement round at all. Callers still typically run the result
// through plan.Validate (see review.go's RenderForReview) so an operator
// sees any structural findings before dispatch, but Skip itself performs
// no validation -- "as-is" means as-is.
func Skip(input string) []byte {
	return bytes.TrimRight([]byte(input), "\n")
}
