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

// TestOperatorTasksNameATransitivelyBlockedDependency closes the one-hop limit
// a FOURTH dogfooding pass found. On Ralph's own plan:
//
//	build   failed
//	race    pending  — cannot run: build failed
//	parity  pending                              <- silent, and just as dead
//
// parity depends on race, race is dead behind build, so parity can never run
// either -- but the check looked exactly one hop and left it reading like a
// healthy queued task. That is the same lie the one-hop fix removed, one level
// deeper, and a real DAG is mostly deeper levels.
func TestOperatorTasksNameATransitivelyBlockedDependency(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-transitive")
	planID := seedReadyGraph(t, s, projectID, "chain", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
		readySpec("parity", "0", "race"),
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
	if got := byID["race"].BlockedByTaskID; got != "build" {
		t.Fatalf("fixture wrong: race should already name build, got %q", got)
	}
	if got := byID["parity"].BlockedByTaskID; got == "" {
		t.Errorf("parity names no blocker; it depends on race, which is dead " +
			"behind build, so it can never run -- yet it reads exactly like a " +
			"healthy queued task")
	}
}

// TestTransitiveBlockerNamesTheRootFailure pins WHICH task gets named. The
// operator needs the task that actually died -- naming the intermediate would
// send them to another pending row to repeat the lookup.
func TestTransitiveBlockerNamesTheRootFailure(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-root")
	planID := seedReadyGraph(t, s, projectID, "deep", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
		readySpec("parity", "0", "race"),
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
	for _, it := range items {
		if it.ID == "parity" && it.BlockedByTaskID != "build" {
			t.Errorf("parity names %q; it must name the task that actually FAILED "+
				"(build), not the intermediate race, which is itself only pending",
				it.BlockedByTaskID)
		}
	}
}

// TestHealthyChainNamesNoBlocker guards the other direction: an unfinished but
// healthy intermediate must NOT be traversed. That chain clears itself as
// upstream work completes, and following it would mark ordinary in-flight work
// as dead -- the failure mode that keeps this marker meaningful.
func TestHealthyChainNamesNoBlocker(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-healthy")
	planID := seedReadyGraph(t, s, projectID, "alive", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
		readySpec("parity", "0", "race"),
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
		if it.BlockedByTaskID != "" {
			t.Errorf("%s names blocker %q while build is merely RUNNING; nothing "+
				"in this chain is dead", it.ID, it.BlockedByTaskID)
		}
	}
}
