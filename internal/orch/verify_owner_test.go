package orch

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// sessionAndWorker creates a distinct session/worker pair. PIDs differ per tag
// so two callers cannot collide on the identity columns.
func sessionAndWorker(t *testing.T, s *store.Store, tag string, pid int) (string, string) {
	t.Helper()
	ctx := context.Background()
	sessionID, err := s.CreateSession(ctx, store.SessionOpts{
		Role: "worker", PID: pid, PIDStartTime: "t0-" + tag,
	})
	if err != nil {
		t.Fatalf("CreateSession(%s): %v", tag, err)
	}
	workerID, err := s.CreateWorker(ctx, store.WorkerOpts{
		SessionID: sessionID, Provider: "claude",
		SubprocessPID: pid + 1000, SubprocessStartTime: "t0-" + tag,
	})
	if err != nil {
		t.Fatalf("CreateWorker(%s): %v", tag, err)
	}
	return sessionID, workerID
}

// TestVerifyAndCompleteRefusesAStaleReportersEvidence closes #248.
//
// store.MarkDone and MarkFailed are correctly owner-guarded: they require
// claimed_by_session to match the session they are GIVEN. The layer above
// defeated that guard — VerifyAndComplete never received the REPORTING
// worker's session, so it read task.ClaimedBySession at verification time and
// passed that to MarkDone.
//
// The sequence that breaks: worker A stalls, the reaper reclaims the task,
// worker B claims it, then A's provider call finally returns. A's evidence was
// written under B's session. The store guard passed — it compared against the
// session it had just been handed — so B's attempt was overwritten and the
// benign-loss path (ErrTaskNotOwnedRunning) never fired. Nothing reported it.
func TestVerifyAndCompleteRefusesAStaleReportersEvidence(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "stale-owner")
	planID := mustCreateTestPlan(t, s, projectID, "stale-owner", "Own", "# Own\n\n- do the thing\n")
	o := New(s)

	// A task must be materialized explicitly; the plan text alone leaves nothing
	// claimable. Acceptance passes so the test isolates the OWNERSHIP guard
	// rather than accidentally testing a failed check.
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do the thing",
		AcceptanceJSON: `{"command":"exit 0"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sessA, workerA := sessionAndWorker(t, s, "A", 4001)
	taskA, err := s.ClaimNextReady(ctx, planID, sessA, workerA)
	if err != nil {
		t.Fatalf("A claim: %v", err)
	}

	// The reaper reclaims A's task, and B claims the SAME task.
	if err := s.ReleaseClaim(ctx, planID, taskA.ID, sessA, "reaped"); err != nil {
		t.Fatalf("release A: %v", err)
	}
	sessB, workerB := sessionAndWorker(t, s, "B", 4002)
	taskB, err := s.ClaimTask(ctx, planID, taskA.ID, sessB, workerB)
	if err != nil {
		t.Fatalf("B claim of %s: %v", taskA.ID, err)
	}
	if taskB.ClaimedBySession != sessB {
		t.Fatalf("B did not take ownership: owner=%q", taskB.ClaimedBySession)
	}

	// A's late evidence arrives. It must NOT complete the task.
	done, err := o.VerifyAndCompleteAs(ctx, planID, taskA.ID, sessA, a2a.Evidence{Ran: "exit 0", ExitCode: 0, Output: "stale worker A result"})
	if err != nil {
		t.Fatalf("VerifyAndCompleteAs: %v", err)
	}
	if done {
		t.Fatal("a stale reporter's evidence completed the task; worker B's attempt " +
			"was overwritten and nothing reported the loss")
	}

	cur, err := s.GetTask(ctx, planID, taskA.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if cur.Status == store.TaskStatusDone {
		t.Fatalf("task is %q; a stale report must not mark it done", cur.Status)
	}
	if cur.ClaimedBySession != sessB {
		t.Errorf("owner = %q, want B (%q) still holding it", cur.ClaimedBySession, sessB)
	}
}

// TestVerifyAndCompleteAcceptsTheCurrentOwner is the control. A guard that
// refused the worker which actually did the work would be worse than the bug.
func TestVerifyAndCompleteAcceptsTheCurrentOwner(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cur-owner")
	planID := mustCreateTestPlan(t, s, projectID, "cur-owner", "Own", "# Own\n\n- do the thing\n")
	o := New(s)

	// A task must be materialized explicitly; the plan text alone leaves nothing
	// claimable. Acceptance passes so the test isolates the OWNERSHIP guard
	// rather than accidentally testing a failed check.
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do the thing",
		AcceptanceJSON: `{"command":"exit 0"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	sess, worker := sessionAndWorker(t, s, "cur", 4010)
	task, err := s.ClaimNextReady(ctx, planID, sess, worker)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	done, err := o.VerifyAndCompleteAs(ctx, planID, task.ID, sess, a2a.Evidence{Ran: "exit 0", ExitCode: 0, Output: "did the work"})
	if err != nil {
		t.Fatalf("VerifyAndCompleteAs: %v", err)
	}
	if !done {
		t.Fatal("the CURRENT owner's evidence was refused; the guard must reject " +
			"only a session that no longer owns the task")
	}
}

// TestDispatchAttributesEvidenceToTheReportingSession guards the WIRING, not
// the guard itself.
//
// VerifyAndCompleteAs can be correct and still do nothing if dispatch keeps
// calling VerifyAndComplete. That is the shape a containment feature already
// shipped in with zero production callers this session: a well-tested lower
// layer makes the whole thing look wired up. So this asserts on the SOURCE,
// which is the only place the omission is visible.
func TestDispatchAttributesEvidenceToTheReportingSession(t *testing.T) {
	src, err := os.ReadFile("orchestrator.go")
	if err != nil {
		t.Fatalf("read orchestrator.go: %v", err)
	}
	text := string(src)

	// Every verification call from dispatch must name a reporting session.
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(trimmed, "o.VerifyAndComplete(") {
			t.Errorf("dispatch calls VerifyAndComplete without a reporting session: %s\n"+
				"a stale worker's result would be written under whoever owns the task "+
				"by then, overwriting its replacement (#248)", trimmed)
		}
	}
	if n := strings.Count(text, "o.VerifyAndCompleteAs(persistCtx"); n != 2 {
		t.Errorf("found %d attributed verification calls, want 2 (per-step and "+
			"fan-out); a new dispatch path may be unattributed", n)
	}
}
