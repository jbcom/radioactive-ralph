package tui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

type fakeDataSource struct {
	status ipc.StatusReply

	plans    []observe.Plan
	progress map[string]progress

	tasksByPlan map[string][]observe.Task

	projectEvents []observe.Event
	taskEvents    map[string][]observe.Event

	// descriptions feeds the opt-in TaskDetail query, keyed by task id.
	descriptions     map[string]string
	detailErr        error
	descriptionCalls int
	gotTaskIDs       []string

	maxEventID   int64
	attachFrames []json.RawMessage
	attachErr    error
	snapshotErr  error

	attachMu       sync.Mutex
	attachAfterIDs []int64
}

func waitAttachCount(t *testing.T, f *fakeDataSource, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.attachMu.Lock()
		got := len(f.attachAfterIDs)
		f.attachMu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Attach was called fewer than %d times", n)
}

func (f *fakeDataSource) afterIDAt(i int) int64 {
	f.attachMu.Lock()
	defer f.attachMu.Unlock()
	return f.attachAfterIDs[i]
}

func (f *fakeDataSource) Snapshot(
	_ context.Context,
	query observe.SnapshotQuery,
) (*observe.Snapshot, error) {
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	reply := &observe.Snapshot{
		SchemaVersion: observe.SchemaVersion,
		CapturedAt:    time.Now().UTC(),
		Project:       observe.Project{ID: query.ProjectID},
		Summary: observe.Summary{
			ActiveWorkerCount: f.status.ActiveWorkers,
			ZeroActiveWorkers: f.status.ActiveWorkers == 0,
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
		EventCursor:  f.maxEventID,
		RecentEvents: observe.EventPage{Items: []observe.Event{}},
	}
	for _, plan := range f.plans {
		if query.PlanID == "" || plan.ID == query.PlanID {
			if value, ok := f.progress[plan.ID]; ok {
				plan.TaskDone = value.Done
				plan.TaskTotal = value.Total
			}
			reply.Plans.Items = append(reply.Plans.Items, plan)
		}
	}
	if query.PlanID != "" {
		for _, task := range f.tasksByPlan[query.PlanID] {
			if query.TaskID == "" || task.ID == query.TaskID {
				reply.Tasks.Items = append(reply.Tasks.Items, task)
			}
		}
	}
	if query.TaskID != "" {
		reply.RecentEvents.Items = append(
			reply.RecentEvents.Items,
			f.taskEvents[query.PlanID+"/"+query.TaskID]...,
		)
	} else {
		reply.RecentEvents.Items = append(
			reply.RecentEvents.Items,
			f.projectEvents...,
		)
	}
	return reply, nil
}

func (f *fakeDataSource) Attach(
	ctx context.Context,
	afterID int64,
	fn func(ipc.AttachEvent) error,
) error {
	f.attachMu.Lock()
	f.attachAfterIDs = append(f.attachAfterIDs, afterID)
	f.attachMu.Unlock()
	for _, raw := range f.attachFrames {
		var event ipc.AttachEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		if err := fn(event); err != nil {
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

var _ DataSource = (*fakeDataSource)(nil)

// TaskDescriptions serves the opt-in per-plan labels. Setting detailErr
// exercises the degrade path: the view must still render, showing task ids.
func (f *fakeDataSource) TaskDescriptions(
	_ context.Context,
	query observe.TaskDescriptionsQuery,
) (observe.TaskDescriptions, error) {
	f.descriptionCalls++
	f.gotTaskIDs = query.TaskIDs
	if f.detailErr != nil {
		return observe.TaskDescriptions{}, f.detailErr
	}
	return observe.TaskDescriptions{PlanID: query.PlanID, ByTask: f.descriptions}, nil
}
