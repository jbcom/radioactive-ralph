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
