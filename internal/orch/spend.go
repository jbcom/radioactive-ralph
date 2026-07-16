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
// project spend (store.spend, rolled up via
// Store.ProjectSpendByProvider) is at or over its configured cap. A
// provider with no configured cap (absent from o.spendCapUSD, or a cap of
// 0) is treated as uncapped.
func (o *Orchestrator) checkSpendCap(ctx context.Context, projectID, providerName string) error {
	capUSD, ok := o.spendCapUSD[providerName]
	if !ok || capUSD <= 0 {
		return nil
	}
	spend, err := o.store.ProjectSpendByProvider(ctx, projectID)
	if err != nil {
		return fmt.Errorf("orch: read project spend: %w", err)
	}
	spent := spend[providerName]
	if spent >= capUSD {
		return &ErrSpendCapExceeded{Provider: providerName, SpentUSD: spent, CapUSD: capUSD}
	}
	return nil
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
