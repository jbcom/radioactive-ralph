package store

import (
	"context"
	"testing"
)

// TestOperatorTasksNameATerminallyBlockedDependency closes the gap a second
// DOGFOODING pass found: a plan whose first task FAILED renders its dependents
// as a bare "pending", byte-identical to tasks that simply have not started.
//
// The distinction is not cosmetic, and it is verified rather than assumed:
// both readiness walks satisfy a dependency only on done/skipped/decomposed,
// MarkFailed retries via pending and lands on failed once retries are
// exhausted, and NO transition leaves failed. So a dependent behind a failed
// task can never run -- the plan is dead while every row still reads pending.
func TestOperatorTasksNameATerminallyBlockedDependency(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-terminal")
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

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	byID := map[string]OperatorTask{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if got := byID["build"].Status; got != TaskStatusFailed {
		t.Fatalf("fixture wrong: build is %q, want failed -- this test proves "+
			"nothing unless the dependency actually failed", got)
	}
	if got := byID["race"].BlockedByTaskID; got != "build" {
		t.Errorf("race reports BlockedByTaskID=%q, want \"build\": it can never "+
			"run, but renders as plain %q -- indistinguishable from a task that "+
			"simply has not started", got, byID["race"].Status)
	}
	// A task with no unsatisfied dependency must not claim one.
	if got := byID["build"].BlockedByTaskID; got != "" {
		t.Errorf("build reports BlockedByTaskID=%q; it has no dependency", got)
	}
}

// TestOperatorTasksIgnoreAnIncompleteDependency is the other half, and the
// reason this field names only TERMINAL blockers.
//
// A dependent behind an unfinished-but-healthy task is the ordinary mid-flight
// state of every running plan. Naming that would put a blocked-looking marker
// on essentially every task in progress, which is how a signal becomes noise
// and stops being read at all -- the same reason partitions of one go
// unlabelled.
func TestOperatorTasksIgnoreAnIncompleteDependency(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-incomplete")
	planID := seedReadyGraph(t, s, projectID, "healthy", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
	})
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "build", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	for _, it := range items {
		if it.ID == "race" && it.BlockedByTaskID != "" {
			t.Errorf("race reports BlockedByTaskID=%q while build is merely "+
				"RUNNING; that dependency clears itself, and flagging it would "+
				"mark every healthy in-flight plan as blocked", it.BlockedByTaskID)
		}
	}
}
