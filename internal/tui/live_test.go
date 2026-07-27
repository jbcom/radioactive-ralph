package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

func TestLiveDataSourceReadsScopedSafeSnapshotThroughSupervisor(t *testing.T) {
	stateRoot := t.TempDir()
	ctx := context.Background()
	st, err := store.Open(
		ctx,
		store.Options{DSN: store.DSN(stateRoot + "/store.db")},
	)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	projectID, err := st.CreateProject(ctx, "tui-project", []store.Fingerprint{{
		Kind:  store.FingerprintKindAbsPath,
		Value: t.TempDir(),
	}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID: projectID,
		Slug:      "safe-plan",
		Title:     "Safe Plan",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for _, taskID := range []string{"done-task", "selected-task"} {
		if err := st.CreateTask(ctx, store.CreateTaskOpts{
			PlanID:         planID,
			ID:             taskID,
			Description:    "private description " + taskID,
			AcceptanceJSON: `["private command"]`,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", taskID, err)
		}
	}
	if _, err := st.DB().ExecContext(ctx, `
		UPDATE tasks SET status = ?
		WHERE plan_id = ? AND id = 'done-task'
	`, string(store.TaskStatusDone), planID); err != nil {
		t.Fatalf("mark task done: %v", err)
	}
	if err := st.Emit(ctx, store.EmitOpts{
		ProjectID:   projectID,
		PlanID:      planID,
		TaskID:      "selected-task",
		Kind:        "task.progress",
		Stream:      "worker",
		Actor:       "private actor",
		PayloadJSON: `{"private":"provider output"}`,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- supervisor.Run(
			runCtx,
			supervisor.Options{RuntimeDir: stateRoot, Store: st},
		)
	}()
	waitForTUISupervisor(t, stateRoot)

	source := NewLiveDataSource(stateRoot, projectID)
	snapshot, err := source.Snapshot(ctx, observe.SnapshotQuery{
		ProjectID:  projectID,
		PlanID:     planID,
		TaskID:     "selected-task",
		PlanLimit:  1,
		TaskLimit:  1,
		EventLimit: 10,
	})
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Plans.Items) != 1 ||
		snapshot.Plans.Items[0].TaskDone != 1 ||
		snapshot.Plans.Items[0].TaskTotal != 2 {
		t.Fatalf("safe plan progress = %+v, want 1/2", snapshot.Plans.Items)
	}
	if len(snapshot.Tasks.Items) != 1 ||
		snapshot.Tasks.Items[0].ID != "selected-task" {
		t.Fatalf("safe scoped tasks = %+v", snapshot.Tasks.Items)
	}
	if len(snapshot.RecentEvents.Items) != 1 ||
		snapshot.EventCursor != snapshot.RecentEvents.Items[0].ID {
		t.Fatalf(
			"safe event page/cursor = (%+v, %d)",
			snapshot.RecentEvents,
			snapshot.EventCursor,
		)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("supervisor exit: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not exit within 3s")
	}
}

func waitForTUISupervisor(t *testing.T, stateRoot string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		client, err := supervisor.Find(stateRoot)
		if err == nil {
			_ = client.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("supervisor did not become reachable")
}
