package store

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

// TestReclaimDoesNotConsumeRetryBudget pins a policy that until now was only an
// accident of implementation.
//
// THE DECISION: a reclaim does not spend retry budget.
//
// The two events are categorically different. defaultTurnRetries bounds
// RETRYABLE PROVIDER FAILURES -- the provider ran, produced a verdict, and the
// verdict was worth another attempt. A reclaim means the worker died: the task
// never got a turn and never produced a verdict at all. Charging it a retry
// spends budget reserved for "the provider tried and failed" on "the machine
// failed", which punishes a task for its host's problem and makes the budget
// mean two different things depending on how it was consumed.
//
// This is what the code already does, because the reaper simply never learned
// about retry_count. That is the defect this test fixes: the behaviour was
// right by accident, so nothing would have caught a change to it. A future
// reader adding `retry_count = retry_count + 1` to the reaper -- which looks
// like an obvious omission -- now fails here instead of silently making every
// reclaimed task fail sooner.
//
// The generosity is NOT unbounded, which is the part that makes this safe. A
// live run had a task claimed 5 times across 2 reclaims and still reach "retry
// budget was exhausted": real provider failures were being counted throughout.
// Reclaims are free; failures are not.
func TestReclaimDoesNotConsumeRetryBudget(t *testing.T) {
	ctx := context.Background()
	clock := clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z"))
	s := openTestStoreWithClock(t, clock)

	projectID := mustCreateProject(t, s, "budget-project")
	planID := mustCreatePlan(t, s, projectID, "budget-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "slow", Description: "a step whose workers keep dying"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Three worker deaths -- one MORE than a default budget of 3 would tolerate
	// if reclaims were charged as retries.
	//
	// A FRESH SESSION per cycle, because advancing the clock past the
	// staleSessionMultiplier window makes the reaper's step 2 delete the old
	// session outright; reusing its id then fails the worker FK. That is the
	// reaper behaving correctly, and reproducing it here is closer to reality
	// than pinning one session alive would be -- a real worker death takes its
	// session with it.
	for i := range 3 {
		sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1 + i, PIDStartTime: "t0"})
		if err != nil {
			t.Fatalf("CreateSession %d: %v", i, err)
		}
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
	if got.ReclaimCount != 3 {
		t.Fatalf("reclaim_count = %d, want 3 (fixture did not reclaim three times)", got.ReclaimCount)
	}
	if got.RetryCount != 0 {
		t.Errorf("retry_count = %d after three RECLAIMS, want 0.\n"+
			"A reclaim means the worker died before the task got a turn; the "+
			"retry budget exists for provider failures that produced a verdict. "+
			"Charging reclaims makes a task fail terminally for its host's "+
			"problem, and makes the same budget mean two different things",
			got.RetryCount)
	}
	// Still runnable: the whole point is that worker deaths do not exhaust it.
	if got.Status != TaskStatusPending {
		t.Errorf("status = %q after three reclaims, want pending -- worker deaths "+
			"must not terminate a task that never got to run", got.Status)
	}
}
