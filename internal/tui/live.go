package tui

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

// liveDataSource is the production DataSource: it forwards every call to
// the supervisor (via a freshly-dialed *ipc.Client per call — see below)
// or the shared *store.Store's existing read methods, plus an
// *orch.Orchestrator used ONLY for PlanProgress (a pure read: it parses
// the plan's stored markdown and diffs it against the store's done-set —
// see orch.Orchestrator.PlanProgress). No write method on Orchestrator or
// Store is ever called from this file, which is the enforcement point for
// the TUI's read-only guarantee (see datasource.go's DataSource doc
// comment).
//
// runtimeDir, not a held *ipc.Client, is what liveDataSource stores:
// ipc.Client's own doc comment states its connections are short-lived —
// one command, one reply, then closed — and internal/ipc/server.go's
// handleConn confirms this server-side (it reads exactly one request,
// writes exactly one response, and closes the connection). A single TUI
// session issues many Status calls over its lifetime (one per
// refreshInterval tick), so each call dials fresh and closes when done;
// holding one connection across ticks would make every call after the
// first fail with a "broken pipe" write error, since the server already
// closed it after the first response.
type liveDataSource struct {
	runtimeDir string
	store      *store.Store
	orch       *orch.Orchestrator
	projectID  string
}

// NewLiveDataSource builds the production DataSource: runtimeDir is the
// directory the supervisor's socket lives under (xdg.StateRoot()), st is
// the shared store, and projectID scopes the plan/event reads.
func NewLiveDataSource(runtimeDir string, st *store.Store, projectID string) DataSource {
	return &liveDataSource{
		runtimeDir: runtimeDir,
		store:      st,
		orch:       orch.New(st),
		projectID:  projectID,
	}
}

// dial opens a fresh, short-lived connection to the supervisor for a
// single call. Callers must Close it when done (defer immediately after
// a successful dial).
func (l *liveDataSource) dial() (*ipc.Client, error) {
	client, err := supervisor.Find(l.runtimeDir)
	if err != nil {
		return nil, fmt.Errorf("tui: dial supervisor: %w", err)
	}
	return client, nil
}

func (l *liveDataSource) Status(ctx context.Context) (ipc.StatusReply, error) {
	client, err := l.dial()
	if err != nil {
		return ipc.StatusReply{}, err
	}
	defer func() { _ = client.Close() }()
	return client.Status(ctx)
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

// MaxEventID returns the project's highest event id (0 if none). The model
// reads it once to seed its resume cursor before the first attach — see the
// DataSource interface doc and Model.attachSeeded.
func (l *liveDataSource) MaxEventID(ctx context.Context) (int64, error) {
	return l.store.MaxEventID(ctx, l.projectID)
}

func (l *liveDataSource) Attach(ctx context.Context, afterID int64, fn func(json.RawMessage) error) error {
	client, err := l.dial()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	// afterID is the model-owned cursor: the initial attach passes the seeded
	// current max ("from now") and a reconnect passes the last processed id, so
	// gap events are delivered. Attach is a pure resume — it does NOT re-seed
	// from MaxEventID here, because a re-seed would forget the cursor the model
	// already holds (the bug this design fixes: a first subscription that ended
	// before yielding a frame would otherwise let a reconnect skip the gap).
	return client.Attach(ctx, ipc.AttachArgs{ProjectID: l.projectID, AfterID: afterID}, fn)
}
