package plandag

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// seedPlanTasks creates a plan with the named tasks (no deps), plus a
// session+variant row. Returns the plan id, session id, and variant id.
// Operator-method tests that need to put a specific task into
// running/blocked/etc. state should claim the task via the returned
// session/variant first.
func seedPlanTasks(t *testing.T, s *Store, slug string, tasks ...string) (planID, sessID, svID string) {
	t.Helper()
	ctx := context.Background()
	planID, err := s.CreatePlan(ctx, CreatePlanOpts{Slug: slug, Title: slug, RepoPath: "/tmp/repo"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for _, id := range tasks {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	sessID, err = s.CreateSession(ctx, SessionOpts{
		Mode: SessionModeDurable, Transport: SessionTransportSocket,
		PID: 7, PIDStartTime: "service-7", Host: "local",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	svID, err = s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: "green",
		SubprocessPID: 7700, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant: %v", err)
	}
	return planID, sessID, svID
}

// seedPlanWithDeps creates a plan with parent→child dep plus an
// isolated "solo" task, plus a session+variant row. Used by tests
// that exercise the dependency-unblocking semantics.
func seedPlanWithDeps(t *testing.T, s *Store, slug string) (planID, sessID, svID string) {
	t.Helper()
	planID, sessID, svID = seedPlanTasks(t, s, slug, "parent", "child", "solo")
	if err := s.AddDep(context.Background(), planID, "child", "parent"); err != nil {
		t.Fatalf("AddDep child→parent: %v", err)
	}
	return
}

// claimTask claims the named task via a fresh session+variant, so a
// test can put a specific task into running state without disturbing
// the shared seed session. Returns the session id used for the claim
// (so the test can MarkDone/MarkBlocked against it).
func claimTask(t *testing.T, s *Store, planID, taskID, variant string) (sessID string) {
	t.Helper()
	ctx := context.Background()
	sessID, err := s.CreateSession(ctx, SessionOpts{
		Mode: SessionModeDurable, Transport: SessionTransportSocket,
		PID: 100, PIDStartTime: "service-100", Host: "local",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	svID, err := s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: variant,
		SubprocessPID: 10100, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant: %v", err)
	}
	claimed, err := s.ClaimNextReady(ctx, planID, variant, sessID, svID)
	if err != nil {
		t.Fatalf("ClaimNextReady %s: %v", taskID, err)
	}
	if claimed.ID != taskID {
		t.Fatalf("claimed %s, want %s", claimed.ID, taskID)
	}
	return sessID
}

// TestOperatorSkipTaskUnblocksDependents confirms that skipping a
// parent task satisfies the dependency predicate for its children,
// mirroring the "done" semantics the Ready query already honors.
func TestOperatorSkipTaskUnblocksDependents(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanWithDeps(t, s, "skip-unblock")

	if err := s.OperatorSkipTask(ctx, planID, "parent", TaskEventPayload{Reason: "dropped scope"}); err != nil {
		t.Fatalf("OperatorSkipTask: %v", err)
	}
	parent, err := s.GetTask(ctx, planID, "parent")
	if err != nil {
		t.Fatalf("GetTask parent: %v", err)
	}
	if parent.Status != TaskStatusSkipped {
		t.Fatalf("parent status = %q, want %q", parent.Status, TaskStatusSkipped)
	}

	// child depends on parent; skipped parents satisfy the Ready
	// predicate, so child should now be ready.
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !containsTask(ready, "child") {
		t.Errorf("after skipping parent, ready set = %+v, want child present", ready)
	}

	// Audit-log row must be emitted with the operator_action payload.
	events, err := s.ListTaskEvents(ctx, planID, "parent", 1)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) == 0 || events[0].EventType != "skipped" {
		t.Fatalf("latest event = %+v, want skipped", events)
	}
	if !strings.Contains(events[0].PayloadJSON, "dropped scope") {
		t.Fatalf("payload = %q, want reason", events[0].PayloadJSON)
	}
}

// TestOperatorSkipTaskRejectsTerminal confirms that skip is a no-op
// (with error) on already-terminal tasks — done/skipped/decomposed
// cannot be re-skipped.
func TestOperatorSkipTaskRejectsTerminal(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanTasks(t, s, "skip-terminal", "solo", "other")

	// Skip once.
	if err := s.OperatorSkipTask(ctx, planID, "solo", TaskEventPayload{}); err != nil {
		t.Fatalf("first skip: %v", err)
	}
	// Second skip must error.
	if err := s.OperatorSkipTask(ctx, planID, "solo", TaskEventPayload{}); err == nil {
		t.Fatal("second skip on already-skipped task should error")
	}

	// Done task also rejects skip. Claim "other" via a fresh session,
	// then MarkDone it.
	sessID := claimTask(t, s, planID, "other", "green")
	if _, err := s.MarkDone(ctx, planID, "other", sessID, `{}`); err != nil {
		t.Fatalf("MarkDone other: %v", err)
	}
	if err := s.OperatorSkipTask(ctx, planID, "other", TaskEventPayload{}); err == nil {
		t.Fatal("skip on done task should error")
	}
}

// TestOperatorDecomposeTaskUnblocksDependents confirms that
// decomposing a parent task also satisfies the Ready predicate
// for dependents — same semantics as done/skipped.
func TestOperatorDecomposeTaskUnblocksDependents(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanWithDeps(t, s, "decompose-unblock")

	if err := s.OperatorDecomposeTask(ctx, planID, "parent", TaskEventPayload{Reason: "split into subtasks"}); err != nil {
		t.Fatalf("OperatorDecomposeTask: %v", err)
	}
	parent, err := s.GetTask(ctx, planID, "parent")
	if err != nil {
		t.Fatalf("GetTask parent: %v", err)
	}
	if parent.Status != TaskStatusDecomposed {
		t.Fatalf("parent status = %q, want %q", parent.Status, TaskStatusDecomposed)
	}

	// child should now be ready.
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !containsTask(ready, "child") {
		t.Errorf("after decomposing parent, ready set = %+v, want child present", ready)
	}

	// Audit log row.
	events, err := s.ListTaskEvents(ctx, planID, "parent", 1)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) == 0 || events[0].EventType != "decomposed" {
		t.Fatalf("latest event = %+v, want decomposed", events)
	}
}

// TestOperatorDecomposeRejectsTerminal confirms decompose is rejected
// on done/skipped/decomposed tasks.
func TestOperatorDecomposeRejectsTerminal(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanTasks(t, s, "decompose-terminal", "solo")

	if err := s.OperatorDecomposeTask(ctx, planID, "solo", TaskEventPayload{}); err != nil {
		t.Fatalf("first decompose: %v", err)
	}
	if err := s.OperatorDecomposeTask(ctx, planID, "solo", TaskEventPayload{}); err == nil {
		t.Fatal("second decompose on already-decomposed task should error")
	}
}

// TestOperatorMarkDoneForceCompletesBlocked confirms the operator
// force-done path closes the operator quartet into a quintet:
// blocked, failed, and approval-gated tasks can be force-completed
// from outside a worker session.
func TestOperatorMarkDoneForceCompletesBlocked(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanTasks(t, s, "mark-done-blocked", "solo")

	// Put "solo" into blocked state via the worker path.
	sessID := claimTask(t, s, planID, "solo", "green")
	if err := s.MarkBlocked(ctx, planID, "solo", sessID, TaskEventPayload{Reason: "stuck"}); err != nil {
		t.Fatalf("MarkBlocked: %v", err)
	}

	// Operator force-done.
	if err := s.OperatorMarkDone(ctx, planID, "solo", TaskEventPayload{Summary: "manual verify"}); err != nil {
		t.Fatalf("OperatorMarkDone: %v", err)
	}
	task, err := s.GetTask(ctx, planID, "solo")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusDone {
		t.Fatalf("status = %q, want %q", task.Status, TaskStatusDone)
	}

	// Audit log row with operator_action=mark_done.
	events, err := s.ListTaskEvents(ctx, planID, "solo", 1)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) == 0 || events[0].EventType != "completed_operator" {
		t.Fatalf("latest event = %+v, want completed_operator", events)
	}
	if !strings.Contains(events[0].PayloadJSON, "manual verify") {
		t.Fatalf("payload = %q, want summary", events[0].PayloadJSON)
	}
}

// TestOperatorMarkDoneRejectsDoneAndDecomposed confirms the guard
// clause: already-done and decomposed tasks cannot be re-completed.
func TestOperatorMarkDoneRejectsDoneAndDecomposed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanTasks(t, s, "mark-done-terminal", "parent", "solo")

	// Decompose parent, then try OperatorMarkDone on it.
	if err := s.OperatorDecomposeTask(ctx, planID, "parent", TaskEventPayload{}); err != nil {
		t.Fatalf("OperatorDecomposeTask: %v", err)
	}
	if err := s.OperatorMarkDone(ctx, planID, "parent", TaskEventPayload{}); err == nil {
		t.Fatal("OperatorMarkDone on decomposed task should error")
	}

	// Complete solo through worker path, then try OperatorMarkDone.
	sessID := claimTask(t, s, planID, "solo", "green")
	if _, err := s.MarkDone(ctx, planID, "solo", sessID, `{}`); err != nil {
		t.Fatalf("MarkDone solo: %v", err)
	}
	if err := s.OperatorMarkDone(ctx, planID, "solo", TaskEventPayload{}); err == nil {
		t.Fatal("OperatorMarkDone on done task should error")
	}
}

// TestListActiveSessionVariants confirms the orphan API actually
// returns rows once a session variant is attached to a plan in this
// repo (via SetSessionVariantTask). This is the test coverage called
// out in the assessment.
func TestListActiveSessionVariants(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, svID := seedPlanTasks(t, s, "list-active", "solo")

	// Attach the seeded variant to the plan via SetSessionVariantTask.
	// ListActiveSessionVariants joins session_variants → plans on
	// current_plan_id, so without this the row is invisible to the
	// repo-scoped query.
	if err := s.SetSessionVariantTask(ctx, svID, planID, "solo"); err != nil {
		t.Fatalf("SetSessionVariantTask: %v", err)
	}

	// Seed another session variant under the same repo+plan.
	sess2, err := s.CreateSession(ctx, SessionOpts{
		Mode: SessionModeDurable, Transport: SessionTransportSocket,
		PID: 8, PIDStartTime: "service-8", Host: "local",
	})
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	sv2, err := s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sess2, VariantName: "red",
		SubprocessPID: 8800, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant red: %v", err)
	}
	if err := s.SetSessionVariantTask(ctx, sv2, planID, "solo"); err != nil {
		t.Fatalf("SetSessionVariantTask red: %v", err)
	}

	rows, err := s.ListActiveSessionVariants(ctx, "/tmp/repo", 50)
	if err != nil {
		t.Fatalf("ListActiveSessionVariants: %v", err)
	}
	if len(rows) < 2 {
		t.Fatalf("got %d rows, want at least 2 (green + red)", len(rows))
	}
	var sawGreen, sawRed bool
	for _, row := range rows {
		if row.VariantName == "green" {
			sawGreen = true
		}
		if row.VariantName == "red" {
			sawRed = true
		}
	}
	if !sawGreen || !sawRed {
		t.Errorf("rows = %+v, want both green and red", rows)
	}

	// A different repo path should return zero rows.
	other, err := s.ListActiveSessionVariants(ctx, "/tmp/other-repo", 50)
	if err != nil {
		t.Fatalf("ListActiveSessionVariants other repo: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("other repo rows = %d, want 0", len(other))
	}
}

// TestReadyAcceptsSkippedAndDecomposed is a focused regression test
// for the dependency predicate: both skipped and decomposed
// predecessors must satisfy Ready, so downstream tasks unblock.
// This is the behavior the new OperatorSkipTask and
// OperatorDecomposeTask rely on.
func TestReadyAcceptsSkippedAndDecomposed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanWithDeps(t, s, "ready-predicate")

	// Skip parent → child should be ready.
	if err := s.OperatorSkipTask(ctx, planID, "parent", TaskEventPayload{}); err != nil {
		t.Fatalf("OperatorSkipTask: %v", err)
	}
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready after skip: %v", err)
	}
	if !containsTask(ready, "child") {
		t.Errorf("after skipping parent, ready = %+v, want child", ready)
	}

	// Claim child, then decompose it. (Child has no dependents here,
	// so this just confirms the transition is clean.)
	sessID := claimTask(t, s, planID, "child", "green")
	if err := s.OperatorDecomposeTask(ctx, planID, "child", TaskEventPayload{}); err != nil {
		t.Fatalf("OperatorDecomposeTask child: %v", err)
	}
	decomp, err := s.GetTask(ctx, planID, "child")
	if err != nil {
		t.Fatalf("GetTask child: %v", err)
	}
	if decomp.Status != TaskStatusDecomposed {
		t.Fatalf("child status = %q, want %q", decomp.Status, TaskStatusDecomposed)
	}
	_ = sessID
}

// TestOperatorFailTaskGuardDecomposed is a focused regression for the
// guard clause that already existed in OperatorFailTask — confirms
// decomposed tasks are rejected by OperatorFailTask now that
// OperatorDecomposeTask can actually drive the transition.
func TestOperatorFailTaskGuardDecomposed(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planID, _, _ := seedPlanTasks(t, s, "fail-guard", "solo", "parent")

	if err := s.OperatorDecomposeTask(ctx, planID, "solo", TaskEventPayload{}); err != nil {
		t.Fatalf("OperatorDecomposeTask: %v", err)
	}
	if err := s.OperatorFailTask(ctx, planID, "solo", TaskEventPayload{}); err == nil {
		t.Fatal("OperatorFailTask on decomposed task should error")
	}

	// And skipped tasks are rejected too.
	if err := s.OperatorSkipTask(ctx, planID, "parent", TaskEventPayload{}); err != nil {
		t.Fatalf("OperatorSkipTask: %v", err)
	}
	if err := s.OperatorFailTask(ctx, planID, "parent", TaskEventPayload{}); err == nil {
		t.Fatal("OperatorFailTask on skipped task should error")
	}
}

func containsTask(tasks []Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}
