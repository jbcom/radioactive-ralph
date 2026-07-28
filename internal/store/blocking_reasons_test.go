package store

import (
	"context"
	"testing"
)

// TestListTaskBlockingReasonsReturnsOnlyBlockedTasks is the bulk reader the
// observe surface needs.
//
// Per-task GetTaskExecutionMetadata would be an N+1 against a snapshot that
// already lists every task, and a snapshot is served on an operator's refresh —
// so the cost lands where it is most visible. It returns only tasks that HAVE a
// reason, because an empty string for every healthy task is payload that says
// nothing.
func TestListTaskBlockingReasonsReturnsOnlyBlockedTasks(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "blocking-reasons")
	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "br", Title: "BR"},
		Tasks: []GraphTaskSpec{
			graphTask("healthy", "fine", "0"),
			graphTask("stuck", "blocked on a capability", "0"),
		},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}

	if err := s.MarkBlockedCapability(ctx, planID, "stuck", "binding lacks native_fanout"); err != nil {
		t.Fatalf("MarkBlockedCapability: %v", err)
	}

	reasons, err := s.ListTaskBlockingReasons(ctx, planID)
	if err != nil {
		t.Fatalf("ListTaskBlockingReasons: %v", err)
	}
	if got := reasons["stuck"]; got != "binding lacks native_fanout" {
		t.Errorf("stuck reason = %q, want the recorded reason", got)
	}
	if _, present := reasons["healthy"]; present {
		t.Errorf("a task with no reason appeared in the map (%v); an empty string "+
			"per healthy task is payload that says nothing", reasons)
	}
}

// TestListTaskBlockingReasonsIsScopedToItsPlan keeps one plan's blocking state
// out of another's snapshot.
func TestListTaskBlockingReasonsIsScopedToItsPlan(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "br-scope")
	planA, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "a", Title: "A"},
		Tasks:          []GraphTaskSpec{graphTask("t", "a task", "0")},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph A: %v", err)
	}
	planB, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "b", Title: "B"},
		Tasks:          []GraphTaskSpec{graphTask("t", "a task", "0")},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph B: %v", err)
	}
	if err := s.MarkBlockedInput(ctx, planA, "t", "digest mismatch"); err != nil {
		t.Fatalf("MarkBlockedInput: %v", err)
	}

	bReasons, err := s.ListTaskBlockingReasons(ctx, planB)
	if err != nil {
		t.Fatalf("ListTaskBlockingReasons B: %v", err)
	}
	if len(bReasons) != 0 {
		t.Errorf("plan B saw %v; task ids repeat across plans, so an unscoped "+
			"query would attribute A's block to B", bReasons)
	}
}
