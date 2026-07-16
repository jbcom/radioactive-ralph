package genesis

import (
	"context"
	"fmt"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// ProviderJuxtaposition is the documented REAL Refiner implementation
// referenced by genesis.go's package doc: it dispatches two provider
// agents against the current draft each round -- a "challenger" that
// looks for gaps/ambiguity and an "integrator" that folds the challenge
// into a tightened plan document -- and reports done once the challenger
// finds nothing left to contest. This is the "team of agents juxtapose
// and challenge each other" mechanism from spec §11, expressed as a
// Refiner so it plugs directly into Refine without Refine itself needing
// to know anything about providers/prompts.
//
// Bindings may be the same provider/model for both roles or different
// ones; using two distinct providers (e.g. claude as integrator, codex as
// challenger) is the configuration that most literally realizes
// "juxtapose", but a single provider run twice with different system
// prompts is equally valid and cheaper.
type ProviderJuxtaposition struct {
	Challenger provider.Runner
	Integrator provider.Runner

	ChallengerBinding provider.Binding
	IntegratorBinding provider.Binding

	WorkingDir string
}

// challengerSystemPrompt asks the challenger agent to find gaps rather
// than write prose -- it returns either "NO GAPS" (verbatim, case-
// sensitive) when the draft is end-to-end complete, or a list of concrete
// gaps/ambiguities/missing steps for the integrator to address. Kept
// short per spec §10's "prompts are minimal and situational" instruction.
const challengerSystemPrompt = `You are reviewing a work plan draft for completeness.
Read the draft plan markdown in the user message. If it fully covers the
work end-to-end with no missing steps, ambiguous ordering, or unstated
assumptions, respond with exactly: NO GAPS
Otherwise, list the specific gaps found, one per line. Do not rewrite the
plan yourself.`

// integratorSystemPrompt asks the integrator to fold the challenger's
// findings into a revised plan document, honoring the plan grammar
// (internal/plan's heading/list convention) so the result stays valid.
const integratorSystemPrompt = `You are refining a work plan document.
The user message contains the current draft plan markdown, followed by a
list of gaps a reviewer found in it (or "NO GAPS" if none). Produce the
complete, revised plan markdown addressing every gap. Follow this
grammar: headings nest work into groups; heading order is dependency
order; under a heading with no sub-headings, an unordered list is
parallel steps and an ordered list is sequential steps. Respond with ONLY
the revised markdown document, nothing else.`

// noGapsMarker is the challenger's exact-match signal that the draft is
// complete. Matched case-sensitively and trimmed of surrounding
// whitespace so a trailing newline from the provider doesn't break
// convergence detection.
const noGapsMarker = "NO GAPS"

// Refine adapts ProviderJuxtaposition to the Refiner func type so it can
// be passed directly to genesis.Refine.
func (j ProviderJuxtaposition) Refine(ctx context.Context, draft string) (next string, done bool, err error) {
	challengeResult, err := j.Challenger.Run(ctx, j.ChallengerBinding, provider.Request{
		WorkingDir:   j.WorkingDir,
		SystemPrompt: challengerSystemPrompt,
		UserPrompt:   draft,
	})
	if err != nil {
		return "", false, fmt.Errorf("genesis: challenger round: %w", err)
	}

	gaps := strings.TrimSpace(challengeResult.AssistantOutput)
	if gaps == noGapsMarker {
		return draft, true, nil
	}

	integrateResult, err := j.Integrator.Run(ctx, j.IntegratorBinding, provider.Request{
		WorkingDir:   j.WorkingDir,
		SystemPrompt: integratorSystemPrompt,
		UserPrompt:   draft + "\n\n---\nReviewer gaps:\n" + gaps,
	})
	if err != nil {
		return "", false, fmt.Errorf("genesis: integrator round: %w", err)
	}

	return strings.TrimSpace(integrateResult.AssistantOutput), false, nil
}
