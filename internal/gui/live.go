package gui

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

type liveController struct {
	runtimeDir string
	projectID  string
}

// NewLiveController builds the safe production GUI controller.
func NewLiveController(runtimeDir, projectID string) Controller {
	return &liveController{runtimeDir: runtimeDir, projectID: projectID}
}

func (l *liveController) dial() (*ipc.Client, error) {
	client, err := supervisor.Find(l.runtimeDir)
	if err != nil {
		return nil, fmt.Errorf("gui: dial supervisor: %w", err)
	}
	return client, nil
}

func (l *liveController) Snapshot(
	ctx context.Context,
	query observe.SnapshotQuery,
) (*observe.Snapshot, error) {
	if query.ProjectID == "" {
		query.ProjectID = l.projectID
	}
	client, err := l.dial()
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	snapshot, err := client.ObserveSnapshot(ctx, query)
	if err != nil {
		if ipc.IsCode(err, ipc.CodeUnsupportedCommand) {
			return nil, fmt.Errorf(
				"gui: supervisor protocol v%d required; upgrade and restart supervisor: %w",
				ipc.QueryProtoVersion,
				err,
			)
		}
		return nil, err
	}
	if err := observe.ValidateSnapshotResponse(snapshot, query); err != nil {
		return nil, fmt.Errorf("gui: unsafe supervisor snapshot: %w", err)
	}
	return snapshot, nil
}

func (l *liveController) Attach(
	ctx context.Context,
	fn func(ipc.AttachEvent) error,
) error {
	seed, err := l.Snapshot(ctx, observe.SnapshotQuery{
		ProjectID:  l.projectID,
		PlanLimit:  1,
		TaskLimit:  1,
		EventLimit: 1,
	})
	if err != nil {
		return fmt.Errorf("gui: seed attach cursor: %w", err)
	}

	client, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.AttachEvents(
		ctx,
		ipc.AttachArgs{
			ProjectID: l.projectID,
			AfterID:   seed.EventCursor,
		},
		fn,
	)
}

func (l *liveController) ImportPlan(
	ctx context.Context,
	args ipc.PlanImportArgs,
) (ipc.PlanImportReply, error) {
	client, err := l.dial()
	if err != nil {
		return ipc.PlanImportReply{}, err
	}
	defer func() { _ = client.Close() }()
	return client.PlanImport(ctx, args)
}

func (l *liveController) SetPlanStatus(
	ctx context.Context,
	planID, status string,
) error {
	client, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	_, err = client.PlanSetStatus(
		ctx,
		ipc.PlanSetStatusArgs{PlanID: planID, Status: status},
	)
	return err
}

func (l *liveController) ApproveTask(
	ctx context.Context,
	planID, taskID string,
) error {
	client, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.TaskApprove(
		ctx,
		ipc.TaskApproveArgs{PlanID: planID, TaskID: taskID},
	)
}

func (l *liveController) KillWorker(
	ctx context.Context,
	workerID string,
) error {
	client, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.WorkerKill(ctx, ipc.WorkerKillArgs{WorkerID: workerID})
}

var _ Controller = (*liveController)(nil)
