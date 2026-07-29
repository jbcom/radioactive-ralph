package store

import (
	"context"
	"fmt"
	"testing"
	"time"
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

// TestTransitiveBlockerSurvivesADeepChain closes a P2 review finding on the
// recursion cap itself: `dead.depth < 64` silently recreated the exact bug this
// projection fixes, just past 64 hops.
//
// The threshold is reachable by ordinary means -- plan import turns an ordered
// group into a 1:1 dependency chain with no depth limit of its own, so a
// 66-step ordered list is enough. Truncating there made the deepest tasks
// render as healthy `pending` again, which is the failure mode the whole
// marker exists to remove.
func TestTransitiveBlockerSurvivesADeepChain(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-deep-chain")

	const chain = 80 // comfortably past the old cap
	specs := []GraphTaskSpec{readySpec("t0", "0")}
	for i := 1; i < chain; i++ {
		specs = append(specs, readySpec(
			fmt.Sprintf("t%d", i), "0", fmt.Sprintf("t%d", i-1)))
	}
	planID := seedReadyGraph(t, s, projectID, "deep", specs)

	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "t0", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if _, err := s.MarkFailed(ctx, planID, "t0", sessionID, "boom", 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	// A page large enough to CONTAIN the deep tasks. The default page is 50, so
	// a first probe of this read "" for tasks the snapshot never returned and
	// looked like truncation everywhere -- pagination, not the recursion.
	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{
		ProjectID: projectID, TaskLimit: 200,
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	items := snap.Tasks.Items
	last := fmt.Sprintf("t%d", chain-1)
	for _, it := range items {
		if it.ID == last && it.BlockedByTaskID != "t0" {
			t.Errorf("%s names blocker %q, want t0: it sits %d edges past a dead "+
				"root and can never run, but renders as healthy pending",
				last, it.BlockedByTaskID, chain-1)
		}
	}
}

// TestTransitiveBlockerTerminatesOnACycle guards the invariant the depth cap
// used to provide. Removing the cap in favour of visited-node tracking is only
// safe if UNION (not UNION ALL) really does stop a cycle from looping forever,
// so this forces one into task_deps directly -- AddDep refuses to create one --
// and asserts the projection still returns.
//
// Empirical rather than assumed: "the DAG has no cycles" is an invariant
// enforced elsewhere, and a projection that could hang the operator surface
// should not depend on it holding.
func TestTransitiveBlockerTerminatesOnACycle(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dep-cycle")
	planID := seedReadyGraph(t, s, projectID, "cyc", []GraphTaskSpec{
		readySpec("a", "0"),
		readySpec("b", "0", "a"),
	})
	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO task_deps(plan_id, task_id, depends_on) VALUES (?,?,?)`,
		planID, "a", "b"); err != nil {
		t.Fatalf("force cycle: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := operatorTasksForTest(ctx, s, projectID)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("operator tasks: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the projection HUNG on a dependency cycle; visited-node " +
			"tracking must terminate without a depth cap")
	}
}

// TestSnapshotBlockerWalkIsNotPerTask guards the P1 a review caught: the first
// blocker walk ran per TASK, re-deriving the same reachability for every row.
// An imported ordered group is a 1:1 dependency chain, so cost grew
// quadratically on exactly the long plans most likely to contain a dead
// ancestor -- 1.3s for 300 tasks, on a query every TUI and GUI refresh runs.
// Walking forward from failures once brought that to ~25ms.
//
// Getting this guard right took three attempts, and the failures are the
// interesting part:
//
//  1. A 2s wall-clock bound PASSED locally at 26ms and FAILED on CI at 2.074s.
//     It was measuring runner speed, not the property it claimed to protect.
//  2. A "doubling cost <= 8x" ratio bound passed against the RESTORED
//     quadratic code. Measured: the per-task walk scales 6.6x per doubling and
//     the forward walk 3.3x -- the forward walk is not linear either, because a
//     chain of n tasks has O(n^2) reachability pairs and the CTE materializes
//     them. Runner noise cannot separate 3.3 from 6.6 reliably.
//  3. What DOES separate them is absolute cost at a fixed size: 25ms vs 1.31s
//     at n=300, a 50x gap. The bound below sits far above the fast path and far
//     below the slow one, so it survives a slow runner and still fails loudly
//     if the per-row walk returns.
func TestSnapshotBlockerWalkIsNotPerTask(t *testing.T) {
	const chain = 300
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "perf-per-task")
	specs := []GraphTaskSpec{readySpec("t0", "0")}
	for i := 1; i < chain; i++ {
		specs = append(specs, readySpec(
			fmt.Sprintf("t%d", i), "0", fmt.Sprintf("t%d", i-1)))
	}
	planID := seedReadyGraph(t, s, projectID, "long", specs)
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "t0", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if _, err := s.MarkFailed(ctx, planID, "t0", sessionID, "boom", 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	q := OperatorSnapshotQuery{ProjectID: projectID, TaskLimit: 200}
	if _, err := s.ReadOperatorSnapshot(ctx, q); err != nil {
		t.Fatalf("warm snapshot: %v", err)
	}
	best, blocked := time.Hour, 0
	for i := 0; i < 3; i++ {
		start := time.Now()
		snap, err := s.ReadOperatorSnapshot(ctx, q)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if d := time.Since(start); d < best {
			best = d
		}
		blocked = 0
		for _, it := range snap.Tasks.Items {
			if it.BlockedByTaskID != "" {
				blocked++
			}
		}
	}

	// "Fast" is meaningless if the query found nothing to do.
	if blocked == 0 {
		t.Fatal("no task reported a blocker; the timing below would be measuring " +
			"a query that did no work")
	}

	// 25ms fast path, 1.31s slow path, measured on this machine. 500ms is 20x
	// the fast path (ample for a loaded runner) and under half the slow one.
	const budget = 500 * time.Millisecond
	if best > budget {
		t.Errorf("best of 3 snapshots over a %d-task chain took %v, over the %v "+
			"budget; the blocker walk is being re-derived per task again "+
			"(that shape measured 1.31s here)",
			chain, best.Round(time.Millisecond), budget)
	}
}
