package store

import (
	"context"
	"testing"
)

func TestAppendAndListMessages(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "a2a-project")
	planID := mustCreatePlan(t, s, projectID, "a2a-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t1", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	_, workerID := mustCreateSessionAndWorker(t, s, "a2a")

	if err := s.AppendMessage(ctx, AppendMessageOpts{
		WorkerID: workerID, PlanID: planID, TaskID: "t1",
		Role: "ROLE_AGENT", ContentJSON: `{"messageId":"m1"}`,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := s.AppendMessage(ctx, AppendMessageOpts{
		PlanID: planID, TaskID: "t1",
		Role: "ROLE_USER", ContentJSON: `{"messageId":"m2"}`,
	}); err != nil {
		t.Fatalf("AppendMessage (no worker): %v", err)
	}

	msgs, err := s.ListMessages(ctx, planID, "t1")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "ROLE_AGENT" || msgs[1].Role != "ROLE_USER" {
		t.Errorf("messages out of expected order: %+v", msgs)
	}
	if msgs[0].WorkerID != workerID {
		t.Errorf("msgs[0].WorkerID = %q, want %q", msgs[0].WorkerID, workerID)
	}
	if msgs[1].WorkerID != "" {
		t.Errorf("msgs[1].WorkerID = %q, want empty (no worker supplied)", msgs[1].WorkerID)
	}
}

func TestAppendMessageRequiresFields(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.AppendMessage(ctx, AppendMessageOpts{}); err == nil {
		t.Fatal("expected an error for an empty AppendMessageOpts")
	}
}

func TestListMessagesEmptyForUnknownTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "a2a-empty-project")
	planID := mustCreatePlan(t, s, projectID, "a2a-empty-plan")

	msgs, err := s.ListMessages(ctx, planID, "no-such-task")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0", len(msgs))
	}
}

// TestA2AMessageCascadesOnTaskDelete confirms a2a_messages rows are
// cleaned up when their owning task is deleted (schema FK ON DELETE
// CASCADE via (plan_id, task_id)).
func TestA2AMessageCascadesOnTaskDelete(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "a2a-cascade-project")
	planID := mustCreatePlan(t, s, projectID, "a2a-cascade-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t1", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.AppendMessage(ctx, AppendMessageOpts{
		PlanID: planID, TaskID: "t1", Role: "ROLE_AGENT", ContentJSON: `{}`,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if _, err := s.DB().ExecContext(ctx, `DELETE FROM tasks WHERE plan_id = ? AND id = ?`, planID, "t1"); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	var count int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM a2a_messages WHERE plan_id = ? AND task_id = ?`, planID, "t1",
	).Scan(&count); err != nil {
		t.Fatalf("count a2a_messages: %v", err)
	}
	if count != 0 {
		t.Errorf("a2a_messages count after task delete = %d, want 0 (cascade)", count)
	}
}
