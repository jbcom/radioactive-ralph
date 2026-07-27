package store

import (
	"context"
	"testing"
)

// TestEventsAfterBlanksCrossProjectPlanIDs closes a project-isolation leak in
// the LIVE stream.
//
// eventProjectScope deliberately lets an explicit project_id win: a
// contradictory row carrying project_id=A but a plan owned by B belongs to A
// alone. That decides which rows are VISIBLE — but the identifiers on such a row
// must not cross the boundary. readOperatorEvents already blanks them via a
// LEFT JOIN; EventsAfter did not, so the Attach stream forwarded B's raw plan
// and task ids into A's live view.
func TestEventsAfterBlanksCrossProjectPlanIDs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	projectA := mustCreateProject(t, s, "project-a")
	projectB := mustCreateProject(t, s, "project-b")
	planB := mustCreatePlan(t, s, projectB, "plan-b")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planB, ID: "b-task", Description: "b"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// The contradictory row: explicitly project A, but naming project B's work.
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO events(project_id, plan_id, task_id, kind, stream, occurred_at)
		VALUES (?, ?, ?, 'worker.cross-project', 'worker', CURRENT_TIMESTAMP)
	`, projectA, planB, "b-task"); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	events, err := s.EventsAfter(ctx, projectA, 0, 50)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}

	var found bool
	for _, ev := range events {
		if ev.Kind != "worker.cross-project" {
			continue
		}
		found = true
		if ev.PlanID != "" {
			t.Errorf("PlanID = %q, want blank — plan %q belongs to project B", ev.PlanID, planB)
		}
		if ev.TaskID != "" {
			t.Errorf("TaskID = %q, want blank alongside the masked plan", ev.TaskID)
		}
	}
	if !found {
		t.Fatal("the explicitly-project-A event was dropped; scoping must still include it")
	}
}

// TestEventsAfterKeepsSameProjectPlanIDs is the other half: masking must not be
// overzealous. An event whose plan really does belong to the requested project
// keeps its identifiers, or the live view loses the linkage it exists to show.
func TestEventsAfterKeepsSameProjectPlanIDs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	projectA := mustCreateProject(t, s, "project-a")
	planA := mustCreatePlan(t, s, projectA, "plan-a")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planA, ID: "a-task", Description: "a"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO events(project_id, plan_id, task_id, kind, stream, occurred_at)
		VALUES (?, ?, ?, 'worker.same-project', 'worker', CURRENT_TIMESTAMP)
	`, projectA, planA, "a-task"); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	events, err := s.EventsAfter(ctx, projectA, 0, 50)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}
	for _, ev := range events {
		if ev.Kind != "worker.same-project" {
			continue
		}
		if ev.PlanID != planA || ev.TaskID != "a-task" {
			t.Fatalf("in-project event lost its linkage: plan=%q task=%q", ev.PlanID, ev.TaskID)
		}
		return
	}
	t.Fatal("in-project event not returned")
}

// TestEventsAfterIncludesPlanLinkedRows keeps the headline case working: the
// lifecycle events inserted with only a plan_id must still reach the stream.
func TestEventsAfterIncludesPlanLinkedRows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	projectA := mustCreateProject(t, s, "project-a")
	planA := mustCreatePlan(t, s, projectA, "plan-a")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planA, ID: "a-task", Description: "a"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, stream, occurred_at)
		VALUES (?, ?, 'task.claimed', 'worker', CURRENT_TIMESTAMP)
	`, planA, "a-task"); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	events, err := s.EventsAfter(ctx, projectA, 0, 50)
	if err != nil {
		t.Fatalf("EventsAfter: %v", err)
	}
	for _, ev := range events {
		if ev.Kind == "task.claimed" && ev.PlanID == planA {
			return
		}
	}
	t.Fatal("a plan-linked event with no project_id was dropped from the live tail")
}
