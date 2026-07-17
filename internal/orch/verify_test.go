package orch

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestVerifyAndCompleteRejectsFailingAcceptanceCommand is THE key test:
// completion is ORCHESTRATOR-VERIFIED, not agent-asserted. The worker's
// Evidence claims success (ExitCode 0, non-empty output) — a
// self-asserting design would trust that and mark the task done. This
// orchestrator instead RE-RUNS the acceptance command itself and rejects
// completion because the command actually fails, proving the worker's
// self-report carries no weight on its own.
func TestVerifyAndCompleteRejectsFailingAcceptanceCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	ctx := context.Background()
	s := newTestStore(t)
	o := New(s)

	projectID := mustCreateTestProject(t, s, "verify-reject-project")
	planID := mustCreateTestPlan(t, s, projectID, "verify-reject-plan", "Ship", "# Ship\n\n- do the thing\n")

	acceptance := `{"command":"exit 1"}`
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do the thing", AcceptanceJSON: acceptance,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	// The worker LIES (or is simply wrong): it self-reports success.
	dishonestEvidence := a2a.Evidence{
		Ran:      "exit 1",
		ExitCode: 0,
		Output:   "all tests passed, definitely done",
	}

	done, err := o.VerifyAndComplete(ctx, planID, "0.0", dishonestEvidence)
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if done {
		t.Fatal("VerifyAndComplete reported done=true for evidence whose acceptance command actually fails — completion must be VERIFIED, not asserted")
	}

	task, err := s.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status == store.TaskStatusDone {
		t.Fatal("task was marked done in the store despite failing re-verification")
	}

	events, err := s.ListTaskEvents(ctx, planID, "0.0", 10)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	foundRejection := false
	for _, ev := range events {
		if ev.Kind == "worker.verification_failed" {
			foundRejection = true
		}
		if ev.Kind == "worker.verified_done" {
			t.Fatal("worker.verified_done was emitted despite failing acceptance")
		}
	}
	if !foundRejection {
		t.Errorf("expected a worker.verification_failed event, got %+v", events)
	}
}

// TestVerifyAndCompleteAcceptsPassingAcceptanceCommand is the mirror case:
// when the orchestrator RE-RUNS the acceptance command and it genuinely
// passes, VerifyAndComplete marks the task done.
func TestVerifyAndCompleteAcceptsPassingAcceptanceCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	ctx := context.Background()
	s := newTestStore(t)
	o := New(s)

	projectID := mustCreateTestProject(t, s, "verify-accept-project")
	planID := mustCreateTestPlan(t, s, projectID, "verify-accept-plan", "Ship", "# Ship\n\n- do the thing\n")

	acceptance := `{"command":"exit 0"}`
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do the thing", AcceptanceJSON: acceptance,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	ev := a2a.Evidence{Ran: "exit 0", ExitCode: 0, Output: "done"}

	done, err := o.VerifyAndComplete(ctx, planID, "0.0", ev)
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if !done {
		t.Fatal("VerifyAndComplete reported done=false for evidence whose acceptance command genuinely passes")
	}

	task, err := s.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusDone {
		t.Errorf("task status = %q, want done", task.Status)
	}

	events, err := s.ListTaskEvents(ctx, planID, "0.0", 10)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "worker.verified_done" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a worker.verified_done event, got %+v", events)
	}
}

// TestVerifyAndCompleteRejectsMissingFile confirms the file-exists
// mechanical acceptance path also re-checks in pure Go rather than
// trusting the worker's FilesChanged claim.
func TestVerifyAndCompleteRejectsMissingFile(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	o := New(s)

	dir := t.TempDir()
	projectID := mustCreateTestProject(t, s, "verify-file-project")
	planID := mustCreateTestPlan(t, s, projectID, "verify-file-plan", "Ship", "# Ship\n\n- write output.txt\n")

	acceptance := `{"file_exists":"output.txt","dir":"` + filepath.ToSlash(dir) + `"}`
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "write output.txt", AcceptanceJSON: acceptance,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	ev := a2a.Evidence{FilesChanged: []string{"output.txt"}, Output: "wrote the file"}

	done, err := o.VerifyAndComplete(ctx, planID, "0.0", ev)
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if done {
		t.Fatal("VerifyAndComplete accepted a FilesChanged claim without the file actually existing on disk")
	}
}

// TestVerifyAndCompleteJudgmentOnlyFallsBackToNonEmptyOutput confirms a
// task with no mechanical acceptance criterion (the common case today,
// since plan markdown doesn't yet carry an acceptance grammar) still
// requires SOME evidence rather than accepting a bare termination signal.
func TestVerifyAndCompleteJudgmentOnlyFallsBackToNonEmptyOutput(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	o := New(s)

	projectID := mustCreateTestProject(t, s, "verify-judgment-project")
	planID := mustCreateTestPlan(t, s, projectID, "verify-judgment-plan", "Ship", "# Ship\n\n- use judgment\n")

	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "use judgment",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	// Empty evidence: a worker that terminated without producing anything
	// must NOT be treated as done.
	done, err := o.VerifyAndComplete(ctx, planID, "0.0", a2a.Evidence{})
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if done {
		t.Fatal("empty evidence (bare termination) must not verify as done")
	}
}

func mustCreateSessionAndWorkerForTest(t *testing.T, s *store.Store) (sessionID, workerID string) {
	t.Helper()
	ctx := context.Background()
	sessionID, err := s.CreateSession(ctx, store.SessionOpts{Role: "worker", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err = s.CreateWorker(ctx, store.WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	return sessionID, workerID
}
