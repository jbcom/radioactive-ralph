package store

import (
	"context"
	"testing"
)

func TestEmitRequiresKind(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.Emit(ctx, EmitOpts{}); err == nil {
		t.Error("Emit with empty Kind: want error, got nil")
	}
}

func TestEmitAndListProjectEvents(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "events-project")

	if err := s.Emit(ctx, EmitOpts{
		ProjectID:   projectID,
		Kind:        "project.created",
		Stream:      "service",
		Actor:       "test",
		PayloadJSON: `{"note":"hello"}`,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if err := s.Emit(ctx, EmitOpts{
		ProjectID: projectID,
		Kind:      "project.touched",
	}); err != nil {
		t.Fatalf("Emit (minimal fields): %v", err)
	}

	events, err := s.ListProjectEvents(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	// Most recent first.
	if events[0].Kind != "project.touched" {
		t.Errorf("events[0].Kind = %q, want project.touched (most recent first)", events[0].Kind)
	}
	if events[1].Kind != "project.created" {
		t.Errorf("events[1].Kind = %q, want project.created", events[1].Kind)
	}
	if events[1].Actor != "test" {
		t.Errorf("events[1].Actor = %q, want test", events[1].Actor)
	}
	if events[1].Stream != "service" {
		t.Errorf("events[1].Stream = %q, want service", events[1].Stream)
	}
	if events[1].ProjectID != projectID {
		t.Errorf("events[1].ProjectID = %q, want %q", events[1].ProjectID, projectID)
	}
}

func TestListProjectEventsDefaultLimit(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "events-limit-project")

	for i := 0; i < 25; i++ {
		if err := s.Emit(ctx, EmitOpts{ProjectID: projectID, Kind: "tick"}); err != nil {
			t.Fatalf("Emit #%d: %v", i, err)
		}
	}

	// limit <= 0 defaults to 20.
	events, err := s.ListProjectEvents(ctx, projectID, 0)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	if len(events) != 20 {
		t.Fatalf("len(events) = %d, want 20 (default limit)", len(events))
	}

	events, err = s.ListProjectEvents(ctx, projectID, 5)
	if err != nil {
		t.Fatalf("ListProjectEvents(limit=5): %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("len(events) = %d, want 5", len(events))
	}
}

func TestListProjectEventsScopesToProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectA := mustCreateProject(t, s, "project-a")
	projectB := mustCreateProject(t, s, "project-b")

	if err := s.Emit(ctx, EmitOpts{ProjectID: projectA, Kind: "a.event"}); err != nil {
		t.Fatalf("Emit A: %v", err)
	}
	if err := s.Emit(ctx, EmitOpts{ProjectID: projectB, Kind: "b.event"}); err != nil {
		t.Fatalf("Emit B: %v", err)
	}

	eventsA, err := s.ListProjectEvents(ctx, projectA, 10)
	if err != nil {
		t.Fatalf("ListProjectEvents(A): %v", err)
	}
	if len(eventsA) != 1 || eventsA[0].Kind != "a.event" {
		t.Fatalf("ListProjectEvents(A) = %+v, want exactly [a.event]", eventsA)
	}
}

func TestEmitWithPlanAndTaskScope(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "events-plantask-project")
	planID := mustCreatePlan(t, s, projectID, "events-plantask-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t1", Description: "d"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.Emit(ctx, EmitOpts{
		ProjectID: projectID,
		PlanID:    planID,
		TaskID:    "t1",
		Kind:      "task.custom",
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	taskEvents, err := s.ListTaskEvents(ctx, planID, "t1", 10)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	found := false
	for _, ev := range taskEvents {
		if ev.Kind == "task.custom" {
			found = true
			if ev.PlanID != planID || ev.TaskID != "t1" {
				t.Errorf("event PlanID/TaskID = %q/%q, want %q/%q", ev.PlanID, ev.TaskID, planID, "t1")
			}
		}
	}
	if !found {
		t.Error("task.custom event not found via ListTaskEvents")
	}
}
