package store

import (
	"context"
	"fmt"
	"strings"
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

// TestSnapshotBlockerWalkIsMaterializedOnce guards the P1 a review caught: the
// first blocker walk ran per TASK, re-deriving the same reachability for every
// row. An imported ordered group is a 1:1 dependency chain, so cost grew
// quadratically on the long plans most likely to contain a dead ancestor --
// 1.3s for 300 tasks, on a query every TUI and GUI refresh runs.
//
// This asserts the SHAPE of the query plan, not wall-clock, after three timing
// bounds failed in three different ways:
//
//  1. 2s absolute: passed locally at 26ms, FAILED CI at 2.074s.
//  2. "doubling costs <= 8x": PASSED against the restored quadratic code. Both
//     shapes are super-linear (6.6x vs 3.3x per doubling) because a chain of n
//     tasks has O(n^2) reachability pairs; runner noise cannot separate those.
//  3. 500ms absolute: FAILED CI at 2.15s. That number is the lesson -- the
//     FIXED code on CI is slower than the QUADRATIC code on a dev machine, so
//     NO absolute threshold can distinguish the two shapes across hardware.
//
// EXPLAIN QUERY PLAN can. A materialized CTE is computed once; a per-row walk
// appears as a CORRELATED SCALAR SUBQUERY. That distinction is exactly the
// regression being guarded and is identical on every machine.
func TestSnapshotBlockerWalkIsMaterializedOnce(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	rows, err := s.DB().QueryContext(ctx,
		"EXPLAIN QUERY PLAN "+operatorTasksQuery,
		"p", "", "", "", "", "", "", "", "", 10)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var plan []string
	for rows.Next() {
		var id, parent, aux int
		var detail string
		if err := rows.Scan(&id, &parent, &aux, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	if len(plan) == 0 {
		t.Fatal("empty query plan; this test would assert nothing")
	}
	joined := strings.Join(plan, "\n")

	// The RECURSIVE walk specifically must be materialized. Other correlated
	// scalar subqueries in this query are fine and expected -- the failure
	// category and partition lookups are single indexed reads per row. What
	// cannot be per-row is the transitive reachability walk, whose cost is
	// proportional to the chain length rather than constant.
	if !strings.Contains(joined, "MATERIALIZE") {
		t.Errorf("no MATERIALIZE in the plan: the recursive blocker walk is being "+
			"re-derived per row, which is the quadratic shape this guards "+
			"against:\n%s", joined)
	}
	if strings.Contains(joined, "RECURSIVE STEP") &&
		!strings.Contains(joined, "MATERIALIZE") {
		t.Errorf("a recursive step runs outside a materialized CTE:\n%s", joined)
	}
}
