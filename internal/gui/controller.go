// Package gui is the Fyne desktop client for radioactive-ralph.
package gui

import (
	"context"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// Controller is the GUI's safe supervisor read boundary plus its v2 drive
// actions. Views receive only observe DTOs; no raw store type crosses this
// interface.
type Controller interface {
	Snapshot(
		ctx context.Context,
		query observe.SnapshotQuery,
	) (*observe.Snapshot, error)
	Attach(ctx context.Context, fn func(ipc.AttachEvent) error) error

	ImportPlan(
		ctx context.Context,
		args ipc.PlanImportArgs,
	) (ipc.PlanImportReply, error)
	SetPlanStatus(ctx context.Context, planID, status string) error
	ApproveTask(ctx context.Context, planID, taskID string) error
	KillWorker(ctx context.Context, workerID string) error
}
