package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

func openTestStoreWithClock(t *testing.T, clock clockwork.Clock) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, Options{
		DSN:   DSN(filepath.Join(t.TempDir(), "store.db")),
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestReclaimStaleRequeuesTask confirms a task claimed by a worker whose
// heartbeat has gone stale is requeued to pending with reclaim_count
// incremented and its claim cleared — this is the reaper the old daemon
// never implemented, so a crashed worker no longer wedges its task forever.
func TestReclaimStaleRequeuesTask(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "reaper-project")
	planID := mustCreatePlan(t, s, projectID, "reaper-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}

	task, err := s.ClaimNextReady(ctx, planID, sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if task.ID != "a" {
		t.Fatalf("claimed %q, want a", task.ID)
	}

	// Advance the clock well past the stale threshold without any
	// heartbeat — simulating a crashed worker.
	clock.Advance(10 * time.Minute)

	reclaimed, err := s.ReclaimStale(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("ReclaimStale reclaimed = %d, want 1", reclaimed)
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusPending {
		t.Errorf("task status = %q, want pending after reclaim", got.Status)
	}
	if got.ClaimedByWorkerID != "" {
		t.Errorf("claimed_by_worker_id = %q, want empty after reclaim", got.ClaimedByWorkerID)
	}
	if got.ReclaimCount != 1 {
		t.Errorf("reclaim_count = %d, want 1", got.ReclaimCount)
	}
}

// TestReclaimStaleLeavesFreshWorkersAlone confirms a task claimed by a
// worker with a recent heartbeat is left untouched.
func TestReclaimStaleLeavesFreshWorkersAlone(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "fresh-project")
	planID := mustCreatePlan(t, s, projectID, "fresh-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	clock.Advance(1 * time.Minute)
	if err := s.HeartbeatWorker(ctx, workerID); err != nil {
		t.Fatalf("HeartbeatWorker: %v", err)
	}
	clock.Advance(1 * time.Minute)

	reclaimed, err := s.ReclaimStale(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if reclaimed != 0 {
		t.Errorf("ReclaimStale reclaimed = %d, want 0 (worker is fresh)", reclaimed)
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusRunning {
		t.Errorf("task status = %q, want still running", got.Status)
	}
}

// TestReclaimStaleDeletesOldWorkersAndSessions confirms workers/sessions
// stale beyond the longer deletion window are removed outright.
func TestReclaimStaleDeletesOldWorkersAndSessions(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}

	// Beyond staleSessionMultiplier * staleAfter.
	clock.Advance(20 * time.Minute)

	if _, err := s.ReclaimStale(ctx, 5*time.Minute); err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}

	var workerCount, sessionCount int
	if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM workers WHERE id = ?", workerID).Scan(&workerCount); err != nil {
		t.Fatalf("count workers: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if workerCount != 0 {
		t.Errorf("worker still present after long staleness, want deleted")
	}
	if sessionCount != 0 {
		t.Errorf("session still present after long staleness, want deleted")
	}
}

// TestReclaimStaleKeepsSessionWithFreshWorker is the regression guard for the
// double-execution hole: a worker's OWN session is not heartbeated by anything
// but the worker's own beat, so a provider turn lasting longer than the
// session-delete window (staleSessionMultiplier * staleAfter) leaves the session
// row stale even while the worker keeps beating. Without the guard, step-2 would
// delete that stale session, CASCADE-delete the still-live worker, NULL its
// running task's claim, and let branch (b) re-dispatch the task to a second
// worker. The reaper must NOT delete a session that still owns a fresh worker,
// must NOT delete that worker, and must NOT reclaim its running task.
func TestReclaimStaleKeepsSessionWithFreshWorker(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "longturn-project")
	planID := mustCreatePlan(t, s, projectID, "longturn-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "long turn"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "worker", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	// Simulate a long turn: advance WELL past the session-delete window
	// (staleSessionMultiplier=3 * 5min = 15min), keeping only the WORKER's
	// heartbeat fresh on each tick — exactly what runWithHeartbeat's worker
	// beat did before it also beat the session. (This test pins the reaper
	// guard directly: even a bare HeartbeatWorker keeps the pair safe.)
	for i := 0; i < 5; i++ {
		clock.Advance(5 * time.Minute)
		if err := s.HeartbeatWorker(ctx, workerID); err != nil {
			t.Fatalf("HeartbeatWorker: %v", err)
		}
		if _, err := s.ReclaimStale(ctx, 5*time.Minute); err != nil {
			t.Fatalf("ReclaimStale: %v", err)
		}
	}

	// The worker and its session must survive, and the task must still be
	// claimed by the live worker (never reclaimed).
	var workerCount, sessionCount int
	if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM workers WHERE id = ?", workerID).Scan(&workerCount); err != nil {
		t.Fatalf("count workers: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if workerCount != 1 {
		t.Errorf("worker count = %d, want 1 (a fresh worker must not be reaped)", workerCount)
	}
	if sessionCount != 1 {
		t.Errorf("session count = %d, want 1 (a session with a fresh worker must not be reaped)", sessionCount)
	}
	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusRunning {
		t.Errorf("task status = %q, want running (a live worker's task must never be reclaimed → double execution)", got.Status)
	}
	if got.ClaimedByWorkerID != workerID {
		t.Errorf("claimed_by_worker_id = %q, want %q (claim must stay with the live worker)", got.ClaimedByWorkerID, workerID)
	}
}

// TestReclaimStaleRequeuesOrphanedTask confirms a 'running' task whose
// claimed_by_worker_id has ALREADY been cascaded to NULL (the worker row
// was deleted by some other path — e.g. an operator force-closing a
// session — independent of ReclaimStale's own step 2) is still reclaimed
// immediately, rather than being invisible to the reclaim WHERE clause
// forever. This is the exact "crash so hard the process never got an
// FK-cascaded cleanup" scenario ReclaimStale's doc comment describes: a
// task can end up 'running' with claimed_by_worker_id already NULL, and
// without this branch it would never be picked up by ANY future reaper
// pass, no matter how stale — a permanent-stall regression this test
// guards against.
func TestReclaimStaleRequeuesOrphanedTask(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "orphan-project")
	planID := mustCreatePlan(t, s, projectID, "orphan-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	// Simulate the worker row disappearing via a path OTHER than
	// ReclaimStale's own step 2 (e.g. CloseSession/operator intervention
	// cascading workers.session_id ON DELETE CASCADE) — this cascades
	// tasks.claimed_by_worker_id to NULL but does NOT touch tasks.status,
	// leaving the task 'running' with no claiming worker at all.
	if _, err := s.DB().ExecContext(ctx, "DELETE FROM workers WHERE id = ?", workerID); err != nil {
		t.Fatalf("delete worker: %v", err)
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask (pre-reclaim sanity check): %v", err)
	}
	if got.Status != TaskStatusRunning || got.ClaimedByWorkerID != "" {
		t.Fatalf("pre-reclaim state = status=%q claimed_by_worker_id=%q, want running/empty (orphaned)", got.Status, got.ClaimedByWorkerID)
	}

	// No time advance needed: the orphaned-claim branch reclaims
	// regardless of staleness, since there is definitionally no worker
	// left to eventually heartbeat.
	reclaimed, err := s.ReclaimStale(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("ReclaimStale reclaimed = %d, want 1 (orphaned task)", reclaimed)
	}

	got, err = s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusPending {
		t.Errorf("task status = %q, want pending after reclaiming an orphaned claim", got.Status)
	}
	if got.ReclaimCount != 1 {
		t.Errorf("reclaim_count = %d, want 1", got.ReclaimCount)
	}

	// The event must name THIS branch, not merely some branch. The first
	// implementation derived the reason in the UPDATE's RETURNING clause, which
	// evaluates against the POST-update row where claimed_by_worker_id has just
	// been nulled -- so every reclaim, stale-heartbeat ones included, was
	// labelled 'orphaned_claim'. A test asserting only that a reason exists, or
	// only checking the stale-heartbeat case, would have passed against that.
	events, err := s.ListProjectEvents(ctx, projectID, 50)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	var payload string
	for _, e := range events {
		if e.Kind == "task.reclaimed" {
			payload = e.PayloadJSON
			break
		}
	}
	if payload == "" {
		t.Fatal("no task.reclaimed event for an orphaned claim")
	}
	if !strings.Contains(payload, "orphaned_claim") {
		t.Errorf("orphaned-claim payload = %q, want it to name orphaned_claim -- "+
			"the two branches must not collapse into one label", payload)
	}
}

// TestReclaimStaleEmitsPerTaskEvent covers the operator question a bare
// reclaim_count cannot answer: WHICH task was reclaimed, and why.
//
// The reaper previously logged one summary row per pass ({"reclaimed":N}),
// which tells an operator that something was requeued but not what. A task
// showing reclaim_count=2 was therefore indistinguishable from a task whose
// worker crashed twice, and the only way to learn more was to read watchdog
// source -- which is exactly how the `race` step's two reclaims were
// diagnosed by hand.
//
// It also distinguishes the two reclaim BRANCHES, which are already distinct
// in the SQL but collapsed into one counter: a stale heartbeat (the worker
// went away) versus an orphaned claim (the worker row is already gone).
func TestReclaimStaleEmitsPerTaskEvent(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "reaper-event-project")
	planID := mustCreatePlan(t, s, projectID, "reaper-event-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "slow", Description: "a long quiet step"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "codex", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	clock.Advance(10 * time.Minute)
	reclaimed, err := s.ReclaimStale(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("ReclaimStale reclaimed = %d, want 1", reclaimed)
	}

	events, err := s.ListProjectEvents(ctx, projectID, 50)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	var found *Event
	for i := range events {
		if events[i].Kind == "task.reclaimed" {
			found = &events[i]
			break
		}
	}
	if found == nil {
		kinds := make([]string, 0, len(events))
		for _, e := range events {
			kinds = append(kinds, e.Kind)
		}
		t.Fatalf("no task.reclaimed event; a summary-only row cannot tell an "+
			"operator WHICH task was requeued. kinds=%v", kinds)
	}
	if found.TaskID != "slow" {
		t.Errorf("task.reclaimed TaskID = %q, want %q -- an event that does not "+
			"name its task is no better than the summary row it replaced",
			found.TaskID, "slow")
	}
	if found.PlanID != planID {
		t.Errorf("task.reclaimed PlanID = %q, want %q", found.PlanID, planID)
	}
	if !strings.Contains(found.PayloadJSON, "stale_heartbeat") {
		t.Errorf("task.reclaimed payload = %q, want it to name the stale_heartbeat "+
			"reason -- the two reclaim branches are distinct in the SQL and must "+
			"stay distinguishable to an operator", found.PayloadJSON)
	}
}

// TestReclaimEventRecordsConcurrencyPressure surfaces the cause a correct
// reason still points away from.
//
// The `race` step's reclaims were never a worker problem: parallel steps starve
// each other, and 30s of work becomes 138s under load. An operator reading
// `reclaimed 2x: stale_heartbeat` gets something TRUE that still points at the
// wrong suspect -- nothing says "you were running six other steps at the time".
//
// Recorded at reclaim time rather than derived later, because by the time
// anyone reads the row the workers are gone and the pressure is unrecoverable.
func TestReclaimEventRecordsConcurrencyPressure(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "pressure-project")
	planID := mustCreatePlan(t, s, projectID, "pressure-plan")
	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// THREE workers claiming concurrently: the shape that starves a long quiet
	// step. All three go stale together, as they would if the machine were
	// saturated rather than one worker having crashed.
	for i, id := range []string{"a", "b", "c"} {
		workerID, err := s.CreateWorker(ctx, WorkerOpts{
			SessionID: sessionID, Provider: "codex", SubprocessPID: 100 + i, SubprocessStartTime: "t0",
		})
		if err != nil {
			t.Fatalf("CreateWorker %s: %v", id, err)
		}
		if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
			t.Fatalf("ClaimNextReady %s: %v", id, err)
		}
	}

	clock.Advance(10 * time.Minute)
	if _, err := s.ReclaimStale(ctx, 5*time.Minute); err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}

	events, err := s.ListProjectEvents(ctx, projectID, 50)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	var payload string
	for _, e := range events {
		if e.Kind == "task.reclaimed" {
			payload = e.PayloadJSON
			break
		}
	}
	if payload == "" {
		t.Fatal("no task.reclaimed event")
	}
	if !strings.Contains(payload, `"concurrent_claims":3`) {
		t.Errorf("payload = %q, want concurrent_claims:3 -- the THREE in-flight "+
			"claims are the point. A field that records 1 (or the post-update 0) "+
			"would satisfy a mere presence check while measuring nothing, which "+
			"is the defect shape this whole session keeps finding", payload)
	}
}
