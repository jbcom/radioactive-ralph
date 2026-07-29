package store

import (
	"context"
	"testing"
)

// TestDeletePlanRemovesItsTasks is the retention primitive the self-test needs.
//
// Each self-test run imports a new plan (unique slug, deliberately -- that is
// what stopped it reporting stale results). Those accumulate, and the operator
// page limit is a HARD 200: at ~12 tasks per run that is roughly 16 runs of
// headroom, so paging defers the problem rather than solving it. Archiving
// cannot help either -- the operator plan query has no status filter, so an
// archived plan still occupies a page slot.
//
// So removal has to be real. Every dependent table cascades through tasks, and
// the store enables foreign_keys per connection, so the delete is one statement
// -- but that is exactly the kind of claim worth pinning rather than assuming.
func TestDeletePlanRemovesItsTasks(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "plan-delete")
	planID := seedReadyGraph(t, s, projectID, "doomed", []GraphTaskSpec{
		readySpec("a", "0"),
		readySpec("b", "0", "a"),
	})
	keepID := seedReadyGraph(t, s, projectID, "keeper", []GraphTaskSpec{
		readySpec("c", "0"),
	})

	if err := s.DeletePlan(ctx, planID); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, p := range snap.Plans.Items {
		if p.ID == planID {
			t.Error("the deleted plan is still in the operator snapshot; removal " +
				"that leaves the row visible buys nothing over archiving")
		}
	}
	var keptSeen bool
	for _, p := range snap.Plans.Items {
		if p.ID == keepID {
			keptSeen = true
		}
	}
	if !keptSeen {
		t.Fatal("deleting one plan removed another; retention must be surgical")
	}

	// Cascade: no orphaned task rows for the deleted plan.
	var orphans int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE plan_id = ?`, planID).Scan(&orphans); err != nil {
		t.Fatalf("count orphan tasks: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d task row(s) survived their plan; the cascade did not fire, "+
			"so the DB grows even after a delete", orphans)
	}
	// And the dependency edges those tasks carried.
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_deps WHERE plan_id = ?`, planID).Scan(&orphans); err != nil {
		t.Fatalf("count orphan deps: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d task_deps row(s) survived their plan", orphans)
	}
}

// TestDeletePlanRefusesAnUnknownPlan keeps a typo from reporting success. A
// retention command that silently accepts a wrong id would let an operator
// believe they pruned something they did not.
func TestDeletePlanRefusesAnUnknownPlan(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	mustCreateProject(t, s, "plan-delete-missing")

	if err := s.DeletePlan(ctx, "no-such-plan"); err == nil {
		t.Error("DeletePlan accepted an unknown plan id; a prune that reports " +
			"success for a typo is worse than one that fails")
	}
}
