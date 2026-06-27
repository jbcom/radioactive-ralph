package plandag

import (
	"context"
	"strings"
	"testing"
)

// TestOperatorSkipTaskUnblocksDependents confirms that skipping a
// parent task satisfies the dependency predicate for its children,
// mirroring the "done" semantics the Ready query already honors.
func TestOperatorSkipTaskUnblocksDependents(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
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
	s := freshStore(t)
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
