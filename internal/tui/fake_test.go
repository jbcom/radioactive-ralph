package tui

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// fakeDataSource is an in-memory DataSource so model tests never need a
// live supervisor or store. Every field is read directly by the
// corresponding method — no hidden state, no goroutines started unless
// Attach is actually called.
type fakeDataSource struct {
	status ipc.StatusReply

	plans    []store.Plan
	progress map[string]orch.Progress

	tasksByPlan map[string][]store.Task

	projectEvents []store.Event
	taskEvents    map[string][]store.Event // keyed by planID+"/"+taskID

	attachFrames []json.RawMessage
	attachErr    error
}

func (f *fakeDataSource) Status(_ context.Context) (ipc.StatusReply, error) {
	return f.status, nil
}

func (f *fakeDataSource) ListPlans(_ context.Context, _ string) ([]store.Plan, error) {
	return f.plans, nil
}

func (f *fakeDataSource) PlanProgress(_ context.Context, planID string) (orch.Progress, error) {
	return f.progress[planID], nil
}

func (f *fakeDataSource) ListTasks(_ context.Context, planID string) ([]store.Task, error) {
	return f.tasksByPlan[planID], nil
}

func (f *fakeDataSource) ListProjectEvents(_ context.Context, _ string, _ int) ([]store.Event, error) {
	return f.projectEvents, nil
}

func (f *fakeDataSource) ListTaskEvents(_ context.Context, planID, taskID string, _ int) ([]store.Event, error) {
	return f.taskEvents[planID+"/"+taskID], nil
}

func (f *fakeDataSource) Attach(ctx context.Context, fn func(json.RawMessage) error) error {
	for _, frame := range f.attachFrames {
		if err := fn(frame); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if f.attachErr != nil {
		return f.attachErr
	}
	<-ctx.Done()
	return ctx.Err()
}

var errFakeAttach = errors.New("fake attach error")
