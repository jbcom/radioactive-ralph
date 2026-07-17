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

func TestMaxEventIDEmptyProjectIsZero(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "maxid-empty-project")

	maxID, err := s.MaxEventID(ctx, projectID)
	if err != nil {
		t.Fatalf("MaxEventID: %v", err)
	}
	if maxID != 0 {
		t.Errorf("MaxEventID on empty project = %d, want 0", maxID)
	}
}

func TestMaxEventIDReturnsHighestForProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectA := mustCreateProject(t, s, "maxid-a")
	projectB := mustCreateProject(t, s, "maxid-b")

	// Emit for A, then B. B's row has the globally-highest id, but
	// MaxEventID(A) must return A's highest, not the table-wide max.
	if err := s.Emit(ctx, EmitOpts{ProjectID: projectA, Kind: "a.event"}); err != nil {
		t.Fatalf("Emit A: %v", err)
	}
	if err := s.Emit(ctx, EmitOpts{ProjectID: projectB, Kind: "b.event"}); err != nil {
		t.Fatalf("Emit B: %v", err)
	}

	events, err := s.ListProjectEvents(ctx, projectA, 10)
	if err != nil {
		t.Fatalf("ListProjectEvents(A): %v", err)
	}
	wantMaxA := events[0].ID

	maxA, err := s.MaxEventID(ctx, projectA)
	if err != nil {
		t.Fatalf("MaxEventID(A): %v", err)
	}
	if maxA != wantMaxA {
		t.Errorf("MaxEventID(A) = %d, want %d (A's own highest, not the table-wide max)", maxA, wantMaxA)
	}
}

func TestEventsAfterReturnsAscendingCappedRowsAfterCursor(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "eventsafter-project")

	// Emit 5 events; capture their ids in emit order.
	var ids []int64
	for i := 0; i < 5; i++ {
		if err := s.Emit(ctx, EmitOpts{ProjectID: projectID, Kind: "tick"}); err != nil {
			t.Fatalf("Emit #%d: %v", i, err)
		}
		// ListProjectEvents is DESC; the head is the row we just wrote.
		latest, err := s.ListProjectEvents(ctx, projectID, 1)
		if err != nil {
			t.Fatalf("ListProjectEvents: %v", err)
		}
		ids = append(ids, latest[0].ID)
	}

	// After the 2nd id, expect exactly ids[2:], ascending.
	got, err := s.EventsAfter(ctx, projectID, ids[1], 100)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("EventsAfter(after ids[1]) len = %d, want 3", len(got))
	}
	for i, ev := range got {
		if ev.ID != ids[2+i] {
			t.Errorf("got[%d].ID = %d, want %d (ascending after cursor)", i, ev.ID, ids[2+i])
		}
	}

	// limit caps the batch and preserves ascending order from the cursor.
	capped, err := s.EventsAfter(ctx, projectID, ids[1], 2)
	if err != nil {
		t.Fatalf("EventsAfter(limit=2): %v", err)
	}
	if len(capped) != 2 || capped[0].ID != ids[2] || capped[1].ID != ids[3] {
		t.Fatalf("EventsAfter(limit=2) = %+v, want ascending [ids[2], ids[3]]", capped)
	}

	// A cursor at the max id yields nothing (no new events).
	none, err := s.EventsAfter(ctx, projectID, ids[4], 100)
	if err != nil {
		t.Fatalf("EventsAfter(at max): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("EventsAfter(at max id) len = %d, want 0", len(none))
	}
}

func TestEventsAfterScopesToProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectA := mustCreateProject(t, s, "eventsafter-a")
	projectB := mustCreateProject(t, s, "eventsafter-b")

	if err := s.Emit(ctx, EmitOpts{ProjectID: projectA, Kind: "a.event"}); err != nil {
		t.Fatalf("Emit A: %v", err)
	}
	if err := s.Emit(ctx, EmitOpts{ProjectID: projectB, Kind: "b.event"}); err != nil {
		t.Fatalf("Emit B: %v", err)
	}

	// From cursor 0, project A sees only its own event even though B's row
	// has a higher id.
	got, err := s.EventsAfter(ctx, projectA, 0, 100)
	if err != nil {
		t.Fatalf("EventsAfter(A): %v", err)
	}
	if len(got) != 1 || got[0].Kind != "a.event" {
		t.Fatalf("EventsAfter(A, 0) = %+v, want exactly [a.event]", got)
	}
}

func TestEventsAfterIncludesPlanScopedEventsWithoutProjectID(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "eventsafter-planlink-project")
	planID := mustCreatePlan(t, s, projectID, "eventsafter-planlink-plan")

	// Emit a task-lifecycle-style event the way tasks.go does: plan_id/task_id
	// set, project_id LEFT EMPTY. A bare project_id filter would drop this — the
	// P1 this scoping fixes.
	if err := s.Emit(ctx, EmitOpts{
		PlanID: planID,
		TaskID: "t1",
		Kind:   "task.claimed",
		Stream: "worker",
	}); err != nil {
		t.Fatalf("Emit plan-scoped: %v", err)
	}
	// And a directly project-scoped event, to confirm both are returned.
	if err := s.Emit(ctx, EmitOpts{ProjectID: projectID, Kind: "service.started"}); err != nil {
		t.Fatalf("Emit project-scoped: %v", err)
	}

	got, err := s.EventsAfter(ctx, projectID, 0, 100)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}
	kinds := map[string]bool{}
	for _, ev := range got {
		kinds[ev.Kind] = true
	}
	if !kinds["task.claimed"] {
		t.Errorf("EventsAfter dropped the plan-scoped task.claimed event (project_id NULL, plan_id set); got kinds %v", kinds)
	}
	if !kinds["service.started"] {
		t.Errorf("EventsAfter dropped the project-scoped service.started event; got kinds %v", kinds)
	}

	// MaxEventID must count the plan-scoped row too, else a fresh attach's
	// cursor would sit below it and the live stream would replay it.
	maxID, err := s.MaxEventID(ctx, projectID)
	if err != nil {
		t.Fatalf("MaxEventID: %v", err)
	}
	if maxID != got[len(got)-1].ID {
		t.Errorf("MaxEventID = %d, want %d (the highest scoped id, including plan-linked)", maxID, got[len(got)-1].ID)
	}
}

func TestEventsAfterExcludesUnscopedServiceRows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "eventsafter-noise-project")

	// A row with neither project_id nor plan_id (e.g. a service-internal tick).
	if err := s.Emit(ctx, EmitOpts{Kind: "tick", Stream: "service"}); err != nil {
		t.Fatalf("Emit unscoped: %v", err)
	}
	if err := s.Emit(ctx, EmitOpts{ProjectID: projectID, Kind: "project.touched"}); err != nil {
		t.Fatalf("Emit scoped: %v", err)
	}

	got, err := s.EventsAfter(ctx, projectID, 0, 100)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}
	for _, ev := range got {
		if ev.Kind == "tick" {
			t.Errorf("EventsAfter returned an unscoped service row (tick); it belongs to no project")
		}
	}
	if len(got) != 1 || got[0].Kind != "project.touched" {
		t.Fatalf("EventsAfter = %+v, want exactly [project.touched]", got)
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
