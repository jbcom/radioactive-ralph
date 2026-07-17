package tui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

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

	maxEventID   int64 // returned by MaxEventID (the initial cursor seed)
	attachFrames []json.RawMessage
	attachErr    error

	// attachMu guards attachAfterIDs, which the Attach goroutine (started by
	// startAttach) writes and the test goroutine reads — see waitAttachCount.
	attachMu       sync.Mutex
	attachAfterIDs []int64 // records the afterID cursor each Attach was called with
}

// waitAttachCount blocks until Attach has been called at least n times (the
// Attach goroutine records each afterID), so a test can read attachAfterIDs
// without racing the goroutine.
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

// afterIDAt returns the afterID the i-th Attach was called with (0-indexed),
// under the lock.
func (f *fakeDataSource) afterIDAt(i int) int64 {
	f.attachMu.Lock()
	defer f.attachMu.Unlock()
	return f.attachAfterIDs[i]
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

func (f *fakeDataSource) MaxEventID(_ context.Context) (int64, error) {
	return f.maxEventID, nil
}

func (f *fakeDataSource) Attach(ctx context.Context, afterID int64, fn func(json.RawMessage) error) error {
	f.attachMu.Lock()
	f.attachAfterIDs = append(f.attachAfterIDs, afterID) // record each attach's cursor
	f.attachMu.Unlock()
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
