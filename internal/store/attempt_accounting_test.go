package store

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

// TestAttemptCountSurvivesAReclaim closes a gap between what the row says and
// what actually happened.
//
// Found on a live self-test run. A task's event history read:
//
//	claimed / claimed / failed_terminal (budget exhausted)
//	claimed / RECLAIMED / claimed / RECLAIMED / claimed / failed_terminal
//
// Five claims, two reclaims -- and retry_count read 0 the whole time. The
// reaper requeues without touching retry_count (only MarkFailed increments it),
// so a reclaimed task returns to pending with a FULL budget and no record that
// it already had turns.
//
// Two things are wrong and they are separable. This test pins the one that is
// unambiguously a defect: an operator cannot reconcile the row with the log.
// Whether a reclaim SHOULD restore the retry budget is a policy question --
// arguably yes, since the worker died before the task got a fair turn -- but
// right now that generosity is a side effect of the reaper not knowing about
// retries, which is not the same as a decision.
func TestAttemptCountSurvivesAReclaim(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "attempt-project")
	planID := mustCreatePlan(t, s, projectID, "attempt-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "slow", Description: "a long quiet step"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Two full claim/reclaim cycles: the worker dies mid-turn, twice.
	for i := range 2 {
		workerID, err := s.CreateWorker(ctx, WorkerOpts{
			SessionID: sessionID, Provider: "codex", SubprocessPID: 100 + i, SubprocessStartTime: "t0",
		})
		if err != nil {
			t.Fatalf("CreateWorker %d: %v", i, err)
		}
		if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
			t.Fatalf("ClaimNextReady %d: %v", i, err)
		}
		clock.Advance(10 * time.Minute)
		if n, err := s.ReclaimStale(ctx, 5*time.Minute); err != nil || n != 1 {
			t.Fatalf("ReclaimStale %d: n=%d err=%v", i, n, err)
		}
	}

	got, err := s.GetTask(ctx, planID, "slow")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ReclaimCount != 2 {
		t.Fatalf("reclaim_count = %d, want 2 (fixture did not reclaim twice)", got.ReclaimCount)
	}
	// The task has been CLAIMED twice and given up both times. An operator
	// reading the row must be able to tell that from the row.
	if got.AttemptCount() != 2 {
		t.Errorf("attempt_count = %d after 2 claimed-and-reclaimed turns, want 2.\n"+
			"retry_count alone reports %d, because the reaper requeues without "+
			"incrementing it -- so a task that burned real turns reads as "+
			"untouched, and the row disagrees with its own event history",
			got.AttemptCount(), got.RetryCount)
	}
}
