package store

import (
	"context"
	"testing"
)

// TestOperatorPlanReportsNoRunnableWork closes the plan-level form of the lie
// the task rows just stopped telling. A FIFTH dogfooding pass showed both of
// Ralph's own plans reporting:
//
//	status=active  task_done=0
//
// while every task in them was failed or permanently unreachable. The plan row
// is the strongest version of the problem: an operator scanning plans sees
// "active, 0 done" and reads it as work in progress.
//
// Derived, not stored: the read path must not mutate durable status, and a
// dispatcher that later requeues work would have to undo it. The tasks already
// carry the truth; this only aggregates what they say.
func TestOperatorPlanReportsNoRunnableWork(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "plan-health-dead")
	planID := seedReadyGraph(t, s, projectID, "dead", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
	})
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "build", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if _, err := s.MarkFailed(ctx, planID, "build", sessionID, "boom", 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Plans.Items) != 1 {
		t.Fatalf("got %d plan(s), want 1", len(snap.Plans.Items))
	}
	if !snap.Plans.Items[0].NoRunnableWork {
		t.Error("a plan whose every task is failed or unreachable reports " +
			"NoRunnableWork=false; it renders as \"active, 0 done\", which an " +
			"operator reads as work in progress")
	}
}

// TestOperatorPlanWithRunnableWorkIsNotFlagged is the other half, and the
// reason this is worth having: a plan mid-flight must NOT be flagged, or the
// signal appears on healthy work and stops being read -- the same failure mode
// that keeps partitions of one unlabelled.
func TestOperatorPlanWithRunnableWorkIsNotFlagged(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "plan-health-alive")
	planID := seedReadyGraph(t, s, projectID, "alive", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
	})
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "build", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Plans.Items[0].NoRunnableWork {
		t.Error("a plan with a RUNNING task was flagged as having no runnable " +
			"work; flagging healthy in-flight plans makes the signal noise")
	}
}

// TestEmptyPlanIsNotFlaggedAsDead pins the BEHAVIOUR, not one mechanism.
//
// A plan with no tasks is awaiting import, not dead, and a marker on every
// freshly-created plan is noise from the first moment an operator sees it.
//
// Two things currently keep it unflagged, and I only learned the second by
// measuring: the explicit TaskTotal > 0 guard, and the fact that a LEFT JOIN
// over zero tasks still yields one row with t.status NULL, which fails
// `status IN (...)` and lands in the ELSE arm -- so runnable is 1, not 0.
// Removing the guard alone does NOT make this test fail, which is exactly why
// the test asserts the outcome rather than the guard: either mechanism
// changing must be caught.
func TestEmptyPlanIsNotFlaggedAsDead(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "plan-health-empty")
	mustCreatePlan(t, s, projectID, "empty")

	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Plans.Items) != 1 {
		t.Fatalf("got %d plan(s), want 1", len(snap.Plans.Items))
	}
	if snap.Plans.Items[0].NoRunnableWork {
		t.Error("an EMPTY plan was flagged as having no runnable work; it is " +
			"awaiting tasks, not dead, and flagging it marks every new plan")
	}
}
