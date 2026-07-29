package store

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

// TestOperatorTaskCarriesReclaimReason closes the last gap in reading a
// reclaimed task.
//
// task.reclaimed events made the REASON durable, but `status` still showed a
// bare reclaim_count on the row. That leaves the fast read as "why is this
// number 2?" with the answer one query away in the events stream -- which is
// the same correlate-by-hand step that made a reclaimed step look stuck in the
// first place.
//
// failure_category already rides along on the row for failed tasks. A reclaimed
// task deserves the same treatment: the row should name its own cause.
func TestOperatorTaskCarriesReclaimReason(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "reclaim-surface-project")
	planID := mustCreatePlan(t, s, projectID, "reclaim-surface-plan")
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
	if _, err := s.ReclaimStale(ctx, 5*time.Minute); err != nil {
		t.Fatalf("ReclaimStale: %v", err)
	}

	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err != nil {
		t.Fatalf("OperatorSnapshot: %v", err)
	}
	var found *OperatorTask
	for i := range snap.Tasks.Items {
		if snap.Tasks.Items[i].ID == "slow" {
			found = &snap.Tasks.Items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("task 'slow' missing from the operator snapshot")
	}
	if found.ReclaimCount != 1 {
		t.Fatalf("reclaim_count = %d, want 1 (fixture did not reclaim)", found.ReclaimCount)
	}
	if found.ReclaimReason != "stale_heartbeat" {
		t.Errorf("ReclaimReason = %q, want %q -- a row showing a reclaim count "+
			"without its cause forces the operator to correlate events by hand",
			found.ReclaimReason, "stale_heartbeat")
	}
}
