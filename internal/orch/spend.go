package orch

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// ErrSpendCapExceeded is returned by checkSpendCap when a provider is over
// its configured cap. DispatchNext treats this as a per-step admission
// refusal, not a fatal error — other ready steps (possibly on an uncapped
// provider) may still dispatch.
type ErrSpendCapExceeded struct {
	Provider string
	SpentUSD float64
	CapUSD   float64
}

func (e *ErrSpendCapExceeded) Error() string {
	return fmt.Sprintf("orch: provider %q spend $%.2f exceeds cap $%.2f", e.Provider, e.SpentUSD, e.CapUSD)
}

// checkSpendCap refuses dispatch for providerName when its accumulated
// project spend (store.spend, rolled up via Store.ProjectSpendByProvider) is at
// or over its configured cap. A provider with no configured cap (absent from
// o.spendCapUSD, or a cap of 0) is treated as uncapped.
//
// On success for a CAPPED provider it atomically RESERVES the dispatch by
// bumping the per-provider in-flight count (guarded by capInFlightMu across the
// whole check-and-reserve), and refuses if a turn is already in flight. Async
// dispatch made the old check-then-launch racy — N concurrent ready steps could
// each read the same sub-cap balance and all launch. Serializing a capped
// provider to one in-flight turn bounds any cap overshoot to a single turn's
// cost (the per-turn cost is unknown until the turn ends, so a tighter reservation
// isn't possible). The caller MUST releaseSpendReservation(providerName) once the
// turn's usage is recorded (or the dispatch fails before launch).
func (o *Orchestrator) checkSpendCap(ctx context.Context, projectID, providerName string) error {
	capUSD, ok := o.spendCapUSD[providerName]
	if !ok || capUSD <= 0 {
		return nil // uncapped: no reservation, no contention
	}

	o.capInFlightMu.Lock()
	defer o.capInFlightMu.Unlock()

	// A turn is already in flight for this capped provider — its cost is not yet
	// recorded, so admitting another could overspend. Refuse (the ready step is
	// retried on a later pass once the in-flight turn settles).
	if o.capInFlight[providerName] > 0 {
		spend, err := o.store.ProjectSpendByProvider(ctx, projectID)
		if err != nil {
			return fmt.Errorf("orch: read project spend: %w", err)
		}
		return &ErrSpendCapExceeded{Provider: providerName, SpentUSD: spend[providerName], CapUSD: capUSD}
	}

	spend, err := o.store.ProjectSpendByProvider(ctx, projectID)
	if err != nil {
		return fmt.Errorf("orch: read project spend: %w", err)
	}
	spent := spend[providerName]
	if spent >= capUSD {
		return &ErrSpendCapExceeded{Provider: providerName, SpentUSD: spent, CapUSD: capUSD}
	}

	// Reserve: this capped provider now has one in-flight turn until its usage is
	// recorded (releaseSpendReservation).
	if o.capInFlight == nil {
		o.capInFlight = map[string]int{}
	}
	o.capInFlight[providerName]++
	return nil
}

// releaseSpendReservation drops the in-flight reservation checkSpendCap took for
// a capped provider. Called once the dispatched turn's usage is recorded (or the
// dispatch is abandoned before launch). A no-op for an uncapped provider (which
// took no reservation).
func (o *Orchestrator) releaseSpendReservation(providerName string) {
	if capUSD, ok := o.spendCapUSD[providerName]; !ok || capUSD <= 0 {
		return
	}
	o.capInFlightMu.Lock()
	if o.capInFlight[providerName] > 0 {
		o.capInFlight[providerName]--
	}
	o.capInFlightMu.Unlock()
}

// recordUsage persists one provider turn's Usage against projectID/
// workerID for spend-cap accounting. Called after a dispatched worker's
// provider.Runner.Run returns, regardless of verification outcome — spend
// is real the moment tokens were billed, independent of whether the work
// was ultimately accepted.
func (o *Orchestrator) recordUsage(ctx context.Context, projectID, workerID, providerName, model string, usage provider.Usage) error {
	return o.store.RecordSpend(ctx, store.RecordSpendOpts{
		ProjectID:    projectID,
		WorkerID:     workerID,
		Provider:     providerName,
		Model:        model,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CachedTokens: usage.CachedInputTokens,
		CostUSD:      usage.CostUSD,
	})
}

// Progress reports done/total step counts for a plan, derived from
// plan.Decompose's notion of what's left. Surfaced to the macro TUI
// (Phase 7) via supervisor status.
type Progress struct {
	Done  int
	Total int
}

// PlanProgress computes Progress for planID by parsing its stored markdown
// and comparing the full step-id universe (plan.Plan.StepIDs) against the
// store's done-set (the same done-set DispatchNext feeds into
// plan.DecomposeRefs).
func (o *Orchestrator) PlanProgress(ctx context.Context, planID string) (Progress, error) {
	storedPlan, err := o.store.GetPlan(ctx, planID)
	if err != nil {
		return Progress{}, fmt.Errorf("orch: load plan: %w", err)
	}
	parsedPlan, err := plan.Parse([]byte(storedPlan.SourceMarkdown))
	if err != nil {
		return Progress{}, fmt.Errorf("orch: parse plan markdown: %w", err)
	}
	done, err := o.doneSet(ctx, planID)
	if err != nil {
		return Progress{}, err
	}
	ids := parsedPlan.StepIDs()
	p := Progress{Total: len(ids)}
	for _, id := range ids {
		if done[id] {
			p.Done++
		}
	}
	return p, nil
}
