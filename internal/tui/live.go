package tui

import (
	"context"
	"encoding/json"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// liveDataSource is the production DataSource: it forwards every call to
// the supervisor's *ipc.Client (Status/Attach) or the shared *store.Store's
// existing read methods, plus an *orch.Orchestrator used ONLY for
// PlanProgress (a pure read: it parses the plan's stored markdown and
// diffs it against the store's done-set — see orch.Orchestrator.
// PlanProgress). No write method on Orchestrator or Store is ever called
// from this file, which is the enforcement point for the TUI's read-only
// guarantee (see datasource.go's DataSource doc comment).
type liveDataSource struct {
	client    *ipc.Client
	store     *store.Store
	orch      *orch.Orchestrator
	projectID string
}

// NewLiveDataSource builds the production DataSource from an already-
// connected supervisor client and the shared store, scoped to projectID.
func NewLiveDataSource(client *ipc.Client, st *store.Store, projectID string) DataSource {
	return &liveDataSource{
		client:    client,
		store:     st,
		orch:      orch.New(st),
		projectID: projectID,
	}
}

func (l *liveDataSource) Status(ctx context.Context) (ipc.StatusReply, error) {
	return l.client.Status(ctx)
}

func (l *liveDataSource) ListPlans(ctx context.Context, projectID string) ([]store.Plan, error) {
	if projectID == "" {
		projectID = l.projectID
	}
	return l.store.ListPlans(ctx, projectID, nil)
}

func (l *liveDataSource) PlanProgress(ctx context.Context, planID string) (orch.Progress, error) {
	return l.orch.PlanProgress(ctx, planID)
}

func (l *liveDataSource) ListTasks(ctx context.Context, planID string) ([]store.Task, error) {
	return l.store.ListTasks(ctx, planID, nil)
}

func (l *liveDataSource) ListProjectEvents(ctx context.Context, projectID string, limit int) ([]store.Event, error) {
	if projectID == "" {
		projectID = l.projectID
	}
	return l.store.ListProjectEvents(ctx, projectID, limit)
}

func (l *liveDataSource) ListTaskEvents(ctx context.Context, planID, taskID string, limit int) ([]store.Event, error) {
	return l.store.ListTaskEvents(ctx, planID, taskID, limit)
}

func (l *liveDataSource) Attach(ctx context.Context, fn func(json.RawMessage) error) error {
	return l.client.Attach(ctx, fn)
}
