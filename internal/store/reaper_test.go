package store

import (
	"context"
	"path/filepath"
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
}
