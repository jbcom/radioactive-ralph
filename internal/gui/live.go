package gui

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

// liveController is the production Controller: reads forward to the shared store
// and a fresh short-lived *ipc.Client per call (via supervisor.Find), and drive
// actions forward to the client's v2 typed methods. It mirrors
// tui.liveDataSource's lifecycle (ipc connections are one-shot: dial, one
// request, close) and adds the drive half the read-only TUI never had.
//
// liveController is Fyne-free, so it lives in the untagged half of the package
// and the wiring test below runs in the normal CGO-off test job.
type liveController struct {
	runtimeDir string
	store      *store.Store
	orch       *orch.Orchestrator
	projectID  string
}

// NewLiveController builds the production Controller. runtimeDir is where the
// supervisor socket lives (xdg.StateRoot()), st is the shared store, and
// projectID scopes the plan/event reads.
func NewLiveController(runtimeDir string, st *store.Store, projectID string) Controller {
	return &liveController{
		runtimeDir: runtimeDir,
		store:      st,
		orch:       orch.New(st),
		projectID:  projectID,
	}
}

// dial opens a fresh, short-lived supervisor connection for one call. Callers
// Close it (defer immediately after a successful dial).
func (l *liveController) dial() (*ipc.Client, error) {
	c, err := supervisor.Find(l.runtimeDir)
	if err != nil {
		return nil, fmt.Errorf("gui: dial supervisor: %w", err)
	}
	return c, nil
}

func (l *liveController) Status(ctx context.Context) (ipc.StatusReply, error) {
	c, err := l.dial()
	if err != nil {
		return ipc.StatusReply{}, err
	}
	defer func() { _ = c.Close() }()
	return c.Status(ctx)
}

func (l *liveController) ListPlans(ctx context.Context, projectID string) ([]store.Plan, error) {
	if projectID == "" {
		projectID = l.projectID
	}
	return l.store.ListPlans(ctx, projectID, nil)
}

func (l *liveController) PlanProgress(ctx context.Context, planID string) (orch.Progress, error) {
	return l.orch.PlanProgress(ctx, planID)
}

func (l *liveController) ListTasks(ctx context.Context, planID string) ([]store.Task, error) {
	return l.store.ListTasks(ctx, planID, nil)
}

func (l *liveController) ListProjectEvents(ctx context.Context, projectID string, limit int) ([]store.Event, error) {
	if projectID == "" {
		projectID = l.projectID
	}
	return l.store.ListProjectEvents(ctx, projectID, limit)
}

func (l *liveController) ListTaskEvents(ctx context.Context, planID, taskID string, limit int) ([]store.Event, error) {
	return l.store.ListTaskEvents(ctx, planID, taskID, limit)
}

func (l *liveController) Attach(ctx context.Context, fn func(json.RawMessage) error) error {
	c, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.Attach(ctx, fn)
}

func (l *liveController) ImportPlan(ctx context.Context, args ipc.PlanImportArgs) (ipc.PlanImportReply, error) {
	c, err := l.dial()
	if err != nil {
		return ipc.PlanImportReply{}, err
	}
	defer func() { _ = c.Close() }()
	return c.PlanImport(ctx, args)
}

func (l *liveController) SetPlanStatus(ctx context.Context, planID, status string) error {
	c, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	_, err = c.PlanSetStatus(ctx, ipc.PlanSetStatusArgs{PlanID: planID, Status: status})
	return err
}

func (l *liveController) ApproveTask(ctx context.Context, planID, taskID string) error {
	c, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.TaskApprove(ctx, ipc.TaskApproveArgs{PlanID: planID, TaskID: taskID})
}

func (l *liveController) KillWorker(ctx context.Context, workerID string) error {
	c, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return c.WorkerKill(ctx, ipc.WorkerKillArgs{WorkerID: workerID})
}

var _ Controller = (*liveController)(nil)
