package tui

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

// liveDataSource keeps only supervisor discovery state. Every read goes through
// a fresh one-shot IPC connection; it never opens or retains the durable store.
type liveDataSource struct {
	runtimeDir string
	projectID  string
}

// NewLiveDataSource builds the safe production TUI source.
func NewLiveDataSource(runtimeDir, projectID string) DataSource {
	return &liveDataSource{runtimeDir: runtimeDir, projectID: projectID}
}

func (l *liveDataSource) dial() (*ipc.Client, error) {
	client, err := supervisor.Find(l.runtimeDir)
	if err != nil {
		return nil, fmt.Errorf("tui: dial supervisor: %w", err)
	}
	return client, nil
}

func (l *liveDataSource) Snapshot(
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
				"tui: supervisor protocol v%d required; upgrade and restart supervisor: %w",
				ipc.QueryProtoVersion,
				err,
			)
		}
		return nil, err
	}
	if err := observe.ValidateSnapshotResponse(snapshot, query); err != nil {
		return nil, fmt.Errorf("tui: unsafe supervisor snapshot: %w", err)
	}
	return snapshot, nil
}

func (l *liveDataSource) Attach(
	ctx context.Context,
	afterID int64,
	fn func(ipc.AttachEvent) error,
) error {
	client, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.AttachEvents(
		ctx,
		ipc.AttachArgs{ProjectID: l.projectID, AfterID: afterID},
		fn,
	)
}

// TaskDescriptions fetches one plan's task labels over the opt-in query. An old
// supervisor returns CodeUnsupportedCommand; the caller degrades to showing
// task ids rather than failing the view, because a missing label is cosmetic
// while a failed render is not.
func (l *liveDataSource) TaskDescriptions(
	ctx context.Context,
	query observe.TaskDescriptionsQuery,
) (observe.TaskDescriptions, error) {
	if query.ProjectID == "" {
		query.ProjectID = l.projectID
	}
	client, err := l.dial()
	if err != nil {
		return observe.TaskDescriptions{}, err
	}
	defer func() { _ = client.Close() }()

	reply, err := client.ObserveTaskDescriptions(ctx, query)
	if err != nil {
		return observe.TaskDescriptions{}, err
	}
	return *reply, nil
}

var _ DataSource = (*liveDataSource)(nil)
