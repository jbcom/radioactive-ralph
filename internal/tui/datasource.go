// Package tui implements the read-only Bubble Tea client described in
// docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md §7:
// "attach/detach is the wrong nomenclature; running the client simply
// shows the supervisor's live state." The client never writes to the
// store and never dispatches work — it only calls read methods on the
// supervisor's IPC client and the shared store. Three drill-down levels
// (macro/meso/micro) navigate the same live snapshot.
package tui

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// DataSource is every read the TUI needs, gathered behind one interface so
// Model can be driven by either the real supervisor client + shared store
// or an in-memory fake in tests. Every method here is READ-ONLY by
// contract: this is the enforcement point for the spec's "read-only"
// guarantee — Model.Update never calls anything but these methods, and
// none of them may mutate durable state. (The real implementation,
// liveDataSource in live.go, is a thin wrapper: it forwards to
// *ipc.Client.Status/Attach and *store.Store's existing List*/Get* read
// methods — it adds no new write surface of its own.)
type DataSource interface {
	// Status returns the supervisor's current status snapshot (worker
	// counts, task counts, recent heartbeat).
	Status(ctx context.Context) (ipc.StatusReply, error)

	// ListPlans returns the known plans for the project this client is
	// scoped to (empty projectID lists across all projects, matching
	// store.Store.ListPlans).
	ListPlans(ctx context.Context, projectID string) ([]store.Plan, error)

	// PlanProgress reports done/total step counts for one plan.
	PlanProgress(ctx context.Context, planID string) (orch.Progress, error)

	// ListTasks returns a plan's tasks.
	ListTasks(ctx context.Context, planID string) ([]store.Task, error)

	// ListProjectEvents returns the most recent events across the whole
	// project, most recent first.
	ListProjectEvents(ctx context.Context, projectID string, limit int) ([]store.Event, error)

	// ListTaskEvents returns the most recent events for one task, most
	// recent first.
	ListTaskEvents(ctx context.Context, planID, taskID string, limit int) ([]store.Event, error)

	// Attach subscribes to the live event stream from afterID. fn is invoked
	// once per event frame until ctx is cancelled or the stream ends. afterID>0
	// RESUMES from a known cursor (a reconnect passes the last id it processed,
	// so events during the disconnect gap are not missed); afterID<=0 seeds from
	// the current max (an initial attach starts from "now", not full history).
	// Attach must not block Model's redraw loop — Run wires it up on its own
	// goroutine (see model.go).
	Attach(ctx context.Context, afterID int64, fn func(json.RawMessage) error) error
}

// refreshMsg is the periodic tick that drives Model's re-fetch. It carries
// no payload; Update reacts to its type only.
type refreshMsg time.Time

// refreshInterval is how often Model re-fetches Status plus the current
// drill level's data. Short enough to feel live, long enough not to
// hammer the store/socket from a single client.
const refreshInterval = 1 * time.Second

// fetchTimeout bounds a single refresh gather. A healthy round trip is
// sub-millisecond; this ceiling exists so a hung/slow supervisor surfaces an
// error (and lets the next tick retry) instead of blocking the in-flight guard
// forever.
const fetchTimeout = 5 * time.Second
