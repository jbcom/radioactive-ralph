package plandag

import (
	"context"
	"strings"
	"testing"
)

// TestOperatorMarkDoneForceCompletesBlocked confirms the operator
// force-done path closes the operator quartet into a quintet:
// blocked, failed, and approval-gated tasks can be force-completed
// from outside a worker session.
func TestOperatorMarkDoneForceCompletesBlocked(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
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

// TestOperatorMarkDoneRejectsTerminal confirms the guard clause:
// already-done, decomposed, and skipped tasks cannot be re-completed.
// skipped is terminal — un-skipping is not allowed.
func TestOperatorMarkDoneRejectsTerminal(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
	planID, _, _ := seedPlanTasks(t, s, "mark-done-terminal", "parent", "solo", "skipme")

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

	// Skip skipme, then try OperatorMarkDone on it.
	if err := s.OperatorSkipTask(ctx, planID, "skipme", TaskEventPayload{}); err != nil {
		t.Fatalf("OperatorSkipTask: %v", err)
	}
	if err := s.OperatorMarkDone(ctx, planID, "skipme", TaskEventPayload{}); err == nil {
		t.Fatal("OperatorMarkDone on skipped task should error")
	}
}

// TestListActiveSessionVariants confirms the orphan API actually
// returns rows once a session variant is attached to a plan in this
// repo (via SetSessionVariantTask). This is the test coverage called
// out in the assessment.
func TestListActiveSessionVariants(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
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
