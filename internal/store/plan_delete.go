package store

import (
	"context"
	"fmt"
)

// DeletePlan removes a plan and everything hanging off it.
//
// It exists for retention, not for correcting mistakes mid-flight: each
// self-test run imports a plan under a unique slug — deliberately, since that
// is what stopped the run reporting a previous run's result — and those
// accumulate. The operator page limit is a hard 200, so paging defers the
// problem by roughly sixteen runs rather than solving it, and archiving cannot
// help at all: the operator plan query filters on project and cursor only, with
// no status predicate, so an archived plan still occupies a page slot.
//
// One statement is enough because every dependent table cascades through tasks
// and the store enables foreign_keys per connection (see store.go). The
// transaction is still here so a future non-cascading table cannot leave a
// half-deleted plan behind.
//
// Deliberately NOT filtered by plan status. A caller pruning old runs knows
// which ids it wants; refusing to delete an active plan would push that policy
// into the store, where it cannot see whether the plan is still wanted.
func (s *Store) DeletePlan(ctx context.Context, planID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin delete plan: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, planID)
	if err != nil {
		return fmt.Errorf("store: delete plan: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete plan rows affected: %w", err)
	}
	if n == 0 {
		// Reuses the package's existing ErrPlanNotFound so a caller can tell
		// "already gone" from "typo" rather than treating both as done.
		return fmt.Errorf("%w: %s", ErrPlanNotFound, planID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit delete plan: %w", err)
	}
	return nil
}
