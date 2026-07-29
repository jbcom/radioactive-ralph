package orch

import (
	"context"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestVerificationOutlivingThePersistBudgetStillMarksTheTask is the regression
// guard for a bug that made a SUCCEEDING step look flaky.
//
// The post-run path runs under persistCtx, a bounded detached context that
// exists so a nearly-complete turn is not lost when the supervisor shuts down.
// Acceptance verification runs inside it -- and acceptance verification RE-RUNS
// THE STEP'S COMMAND, which for a real plan can be a test suite.
//
// Observed on a live self-test: the `race` step's acceptance command is
// `go test -race -v ./internal/store/`, measured at 30s warm and 138s under
// load, against a 30-SECOND budget. It cannot fit. persistCtx expires, the task
// is never marked, and it sits `running` with a dead heartbeat (the heartbeat
// goroutine stops the moment the turn returns) until the reaper reclaims it at
// 90s. It reclaimed six times in one run.
//
// The task's WORK HAD SUCCEEDED every time. What failed was verifying it inside
// a budget sized for store writes.
//
// This test uses a checker that sleeps past the old 30s bound. If verification
// is still charged to the same budget, the task never reaches a terminal state.
func TestVerificationOutlivingThePersistBudgetStillMarksTheTask(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// A checker slower than the store-write budget, standing in for a real
	// acceptance command that re-runs a test suite.
	slow := func(ctx context.Context, _ string, _ string, _ a2a.Evidence) (bool, string, error) {
		select {
		case <-time.After(2 * time.Second):
			return true, "", nil
		case <-ctx.Done():
			// The bug: verification is cancelled, so the task is never marked.
			return false, "", ctx.Err()
		}
	}
	o := New(s, WithAcceptanceChecker(slow))

	projectID := mustCreateTestProject(t, s, "budget-project")
	planID := mustCreateTestPlan(t, s, projectID, "budget-plan", "Ship", "# Ship\n\n- do the thing\n")
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do the thing",
		AcceptanceJSON: `{"command":"exit 0"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	// THE CALLER'S DEADLINE is the whole point. The real post-run path hands
	// VerifyAndComplete a persistCtx bounded for store writes; passing a
	// context.Background() here would test nothing, because there would be no
	// deadline for verification to outlive. (The first version of this test did
	// exactly that and passed against the unfixed code.)
	persistCtx, cancelPersist := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancelPersist()

	done, err := o.VerifyAndComplete(persistCtx, planID, "0.0",
		a2a.Evidence{Ran: "exit 0", ExitCode: 0, Output: "done"})
	if err != nil {
		t.Fatalf("VerifyAndComplete returned an error for a step whose work "+
			"SUCCEEDED and whose acceptance passes -- only slowly: %v", err)
	}
	if !done {
		t.Fatal("verification that outlived the persist budget did not complete " +
			"the task; it stays 'running' with a dead heartbeat until the reaper " +
			"requeues work that already succeeded")
	}

	got, err := s.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TaskStatusDone {
		t.Errorf("task status = %q, want done -- a slow acceptance command must "+
			"not leave the task unmarked", got.Status)
	}
}

// TestVerificationKeepsTheHeartbeatAlive is the other half, and without it the
// budget fix makes things WORSE.
//
// The heartbeat goroutine stops the instant the provider turn returns.
// Verification runs after that, so a long acceptance command previously ran
// with nothing beating -- and giving it a 10-minute budget against a 90-second
// stale threshold guarantees the reclaim it was meant to prevent. The reaper
// requeues the task, the owner-guarded MarkDone becomes a benign no-op, and
// successful work is silently lost.
//
// A reviewer caught this on the fix itself: the budget and the heartbeat have
// to move together.
//
// Counts BEATS rather than comparing stored timestamps. last_heartbeat is
// second-resolution, so a sub-second test cannot tell "never beat" from "beat
// twice within one second" -- the first version asserted exactly that and
// failed for a reason unrelated to the behaviour.
func TestVerificationKeepsTheHeartbeatAlive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	slow := func(ctx context.Context, _ string, _ string, _ a2a.Evidence) (bool, string, error) {
		select {
		case <-time.After(2500 * time.Millisecond):
			return true, "", nil
		case <-ctx.Done():
			return false, "", ctx.Err()
		}
	}
	o := New(s, WithAcceptanceChecker(slow), WithHeartbeatInterval(200*time.Millisecond))

	projectID := mustCreateTestProject(t, s, "beat-project")
	planID := mustCreateTestPlan(t, s, projectID, "beat-plan", "Ship", "# Ship\n\n- do the thing\n")
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do the thing",
		AcceptanceJSON: `{"command":"exit 0"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	var before string
	if err := s.DB().QueryRowContext(ctx,
		"SELECT last_heartbeat FROM workers WHERE id = ?", workerID).Scan(&before); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}

	if _, err := o.VerifyAndComplete(ctx, planID, "0.0",
		a2a.Evidence{Ran: "exit 0", ExitCode: 0, Output: "done"}); err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}

	// 2.5s of verification, long enough for last_heartbeat's SECOND resolution
	// to move. The first version ran 300ms and compared timestamps, which cannot
	// distinguish "never beat" from "beat twice inside one second".
	var after string
	if err := s.DB().QueryRowContext(ctx,
		"SELECT last_heartbeat FROM workers WHERE id = ?", workerID).Scan(&after); err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if after <= before {
		t.Errorf("worker heartbeat did not advance across a 2.5s verification "+
			"(before=%s after=%s); a long acceptance check runs unprotected and "+
			"the reaper reclaims the task out from under it", before, after)
	}
}
