package plandag

import (
	"context"
	"testing"
)

// TestOperatorDecomposeTaskUnblocksDependents confirms that
// decomposing a parent task also satisfies the Ready predicate
// for dependents — same semantics as done/skipped.
func TestOperatorDecomposeTaskUnblocksDependents(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
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
	s := freshStore(t)
	planID, _, _ := seedPlanTasks(t, s, "decompose-terminal", "solo")

	if err := s.OperatorDecomposeTask(ctx, planID, "solo", TaskEventPayload{}); err != nil {
		t.Fatalf("first decompose: %v", err)
	}
	if err := s.OperatorDecomposeTask(ctx, planID, "solo", TaskEventPayload{}); err == nil {
		t.Fatal("second decompose on already-decomposed task should error")
	}
}

// TestOperatorFailTaskGuardDecomposed is a focused regression for the
// guard clause that already existed in OperatorFailTask — confirms
// decomposed and skipped tasks are rejected by OperatorFailTask now
// that OperatorDecomposeTask / OperatorSkipTask can drive those
// transitions.
func TestOperatorFailTaskGuardDecomposed(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
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

// TestReadyAcceptsSkippedAndDecomposed is a focused regression test
// for the dependency predicate: both skipped and decomposed
// predecessors must satisfy Ready, so downstream tasks unblock.
func TestReadyAcceptsSkippedAndDecomposed(t *testing.T) {
	ctx := context.Background()
	s := freshStore(t)
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
	if sessID := claimTask(t, s, planID, "child", "green"); sessID != "" {
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
	}
}
