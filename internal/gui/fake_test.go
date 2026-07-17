//go:build gui

package gui

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// fakeController is an in-memory Controller for the view tests: it returns
// scripted reads and RECORDS every drive call so a test can assert the widgets
// invoked the right action with the right ids, without a supervisor or a store.
// It mirrors the role tui.fakeDataSource plays for the TUI.
type fakeController struct {
	mu sync.Mutex

	status  ipc.StatusReply
	plans   []store.Plan
	tasks   map[string][]store.Task // planID -> tasks
	progr   map[string]orch.Progress
	pEvents []store.Event
	tEvents map[string][]store.Event // planID+"/"+taskID -> events

	// recorded drive calls
	imported   []ipc.PlanImportArgs
	setStatus  [][2]string // {planID, status}
	approved   [][2]string // {planID, taskID}
	killed     []string    // workerIDs
	importErr  error
	statusErr  error
	approveErr error
	killErr    error
}

func newFakeController() *fakeController {
	return &fakeController{
		tasks:   map[string][]store.Task{},
		progr:   map[string]orch.Progress{},
		tEvents: map[string][]store.Event{},
	}
}

func (f *fakeController) Status(context.Context) (ipc.StatusReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeController) ListPlans(context.Context, string) ([]store.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plans, nil
}

func (f *fakeController) PlanProgress(_ context.Context, planID string) (orch.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.progr[planID], nil
}

func (f *fakeController) ListTasks(_ context.Context, planID string) ([]store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks[planID], nil
}

func (f *fakeController) ListProjectEvents(_ context.Context, _ string, _ int) ([]store.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pEvents, nil
}

func (f *fakeController) ListTaskEvents(_ context.Context, planID, taskID string, _ int) ([]store.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tEvents[planID+"/"+taskID], nil
}

func (f *fakeController) Attach(ctx context.Context, _ func(json.RawMessage) error) error {
	// The fake has no live stream; block until cancelled so the app's attach
	// goroutine behaves like the real one (ends on ctx cancel).
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeController) ImportPlan(_ context.Context, args ipc.PlanImportArgs) (ipc.PlanImportReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imported = append(f.imported, args)
	if f.importErr != nil {
		return ipc.PlanImportReply{}, f.importErr
	}
	return ipc.PlanImportReply{PlanID: "p-" + args.Slug, Slug: args.Slug, Title: args.Title}, nil
}

func (f *fakeController) SetPlanStatus(_ context.Context, planID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setStatus = append(f.setStatus, [2]string{planID, status})
	return f.statusErr
}

func (f *fakeController) ApproveTask(_ context.Context, planID, taskID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved = append(f.approved, [2]string{planID, taskID})
	return f.approveErr
}

func (f *fakeController) KillWorker(_ context.Context, workerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, workerID)
	return f.killErr
}

// snapshot helpers for assertions (take the lock).
func (f *fakeController) killedWorkers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.killed...)
}

func (f *fakeController) approvedTasks() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.approved...)
}

func (f *fakeController) setStatusCalls() [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][2]string(nil), f.setStatus...)
}

var _ Controller = (*fakeController)(nil)
