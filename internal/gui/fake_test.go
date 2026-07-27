//go:build gui

package gui

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
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
	setPlanErr error  // returned by SetPlanStatus() — a drive-action failure
	onSetPlan  func() // optional hook run inside SetPlanStatus, before it returns
	approveErr error
	killErr    error

	// attachCount records how many times Attach was called — so a test can
	// assert runAttach re-dials after the stream ends. attachReturn, when set,
	// makes Attach return immediately with that error (simulating a stream end /
	// failed dial) instead of blocking until ctx cancel.
	attachCount  atomic.Int32
	attachReturn error
}

func newFakeController() *fakeController {
	return &fakeController{
		tasks:   map[string][]store.Task{},
		progr:   map[string]orch.Progress{},
		tEvents: map[string][]store.Event{},
	}
}

func (f *fakeController) Snapshot(
	_ context.Context,
	query observe.SnapshotQuery,
) (*observe.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := &observe.Snapshot{
		SchemaVersion: observe.SchemaVersion,
		CapturedAt:    time.Now().UTC(),
		Project:       observe.Project{ID: query.ProjectID},
		Summary: observe.Summary{
			ActiveWorkerCount: f.status.ActiveWorkers,
			ZeroActiveWorkers: f.status.ActiveWorkers == 0,
			PlanTotal:         len(f.plans),
			TaskStatusCounts: []observe.StatusCount{
				{Status: "ready", Count: f.status.ReadyTasks},
				{Status: "ready_pending_approval", Count: f.status.ApprovalTasks},
				{Status: "blocked", Count: f.status.BlockedTasks},
				{Status: "running", Count: f.status.RunningTasks},
				{Status: "failed", Count: f.status.FailedTasks},
			},
			PlanStatusCounts: []observe.StatusCount{},
		},
		Plans:        observe.PlanPage{Items: []observe.Plan{}},
		Tasks:        observe.TaskPage{Items: []observe.Task{}},
		Workers:      []observe.Worker{},
		RecentEvents: observe.EventPage{Items: []observe.Event{}},
	}
	for _, item := range f.plans {
		if query.PlanID != "" && item.ID != query.PlanID {
			continue
		}
		value := f.progr[item.ID]
		reply.Plans.Items = append(reply.Plans.Items, observe.Plan{
			ID:        item.ID,
			Slug:      item.Slug,
			Title:     item.Title,
			Status:    string(item.Status),
			TaskDone:  value.Done,
			TaskTotal: value.Total,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	if query.PlanID != "" {
		for _, item := range f.tasks[query.PlanID] {
			if query.TaskID != "" && item.ID != query.TaskID {
				continue
			}
			reply.Tasks.Items = append(reply.Tasks.Items, observe.Task{
				PlanID:            item.PlanID,
				ID:                item.ID,
				CanonicalID:       item.PlanID + ":" + item.ID,
				Status:            string(item.Status),
				ClaimedByWorkerID: item.ClaimedByWorkerID,
				CreatedAt:         item.CreatedAt,
				UpdatedAt:         item.UpdatedAt,
			})
		}
	}
	rawEvents := f.pEvents
	if query.TaskID != "" {
		rawEvents = f.tEvents[query.PlanID+"/"+query.TaskID]
	}
	for _, item := range rawEvents {
		reply.RecentEvents.Items = append(
			reply.RecentEvents.Items,
			observe.Event{
				ID:         item.ID,
				PlanID:     item.PlanID,
				TaskID:     item.TaskID,
				Kind:       item.Kind,
				Stream:     item.Stream,
				OccurredAt: item.OccurredAt,
			},
		)
		if item.ID > reply.EventCursor {
			reply.EventCursor = item.ID
		}
	}
	for _, worker := range f.status.Workers {
		reply.Workers = append(reply.Workers, observe.Worker{
			ID: worker.WorkerID,
			Claims: []observe.WorkerClaim{{
				PlanID: worker.PlanID,
				TaskID: worker.TaskID,
				Status: "running",
			}},
		})
	}
	return reply, nil
}

func (f *fakeController) Attach(
	ctx context.Context,
	_ func(ipc.AttachEvent) error,
) error {
	f.attachCount.Add(1)
	// When attachReturn is set, return immediately (simulating a failed dial or
	// a stream end) so a test can observe runAttach re-dialing. Otherwise the
	// fake has no live stream; block until cancelled so the app's attach
	// goroutine behaves like the real one (ends on ctx cancel).
	if f.attachReturn != nil {
		return f.attachReturn
	}
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
	f.setStatus = append(f.setStatus, [2]string{planID, status})
	hook := f.onSetPlan
	err := f.setPlanErr
	f.mu.Unlock()
	// onSetPlan runs after recording but before returning, letting a test simulate
	// the operator navigating away while this RPC is "in flight".
	if hook != nil {
		hook()
	}
	return err
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
