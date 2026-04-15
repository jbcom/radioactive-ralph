package main

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
)

// requireActivePlan enforces the plans-first discipline: every
// variant except fixit refuses to run unless at least one plan with
// status='active' exists in the plandag store. Fixit is the sole
// creator of plans and bypasses this check.
//
// Replaces the former markdown-file gate (plans/index.md). Plans
// now live in SQLite under $XDG_STATE_HOME/radioactive_ralph/.
func requireActivePlan(ctx context.Context) error {
	store, err := openPlanStore(ctx)
	if err != nil {
		return fmt.Errorf("plans-first discipline: %w", err)
	}
	defer store.Close()

	plans, err := store.ListPlans(ctx, []plandag.PlanStatus{plandag.PlanStatusActive})
	if err != nil {
		return fmt.Errorf("plans-first discipline: query plans: %w", err)
	}
	if len(plans) == 0 {
		return fmt.Errorf(
			"plans-first discipline: no active plan found; run `radioactive_ralph run --variant fixit --advise` to create one",
		)
	}
	return nil
}
