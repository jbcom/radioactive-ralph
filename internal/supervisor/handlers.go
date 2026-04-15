package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jbcom/radioactive-ralph/internal/db"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

var (
	errDBClosed             = errors.New("supervisor: event log closed")
	errReloadNotImplemented = errors.New("supervisor: reload-config not yet implemented (M2 stub)")
)

// handler implements ipc.Handler against a live Supervisor. The
// indirection keeps the Supervisor's public surface free of IPC
// specifics — the supervisor talks to its own state, the handler
// translates IPC calls into supervisor operations.
type handler struct {
	sup *Supervisor
}

// HandleStatus answers the CmdStatus request with live supervisor stats.
func (h *handler) HandleStatus(ctx context.Context) (ipc.StatusReply, error) {
	uptime := time.Since(h.sup.started)

	var queued, running int
	if h.sup.db != nil {
		if tasks, err := h.sup.db.ListTasks(ctx, "queued"); err == nil {
			queued = len(tasks)
		}
		if tasks, err := h.sup.db.ListTasks(ctx, "running"); err == nil {
			running = len(tasks)
		}
	}

	var active int
	if h.sup.db != nil {
		if sessions, err := h.sup.db.ActiveSessions(ctx); err == nil {
			active = len(sessions)
		}
	}

	return ipc.StatusReply{
		Variant:        string(h.sup.opts.Variant.Name),
		PID:            os.Getpid(),
		Uptime:         uptime,
		ActiveSessions: active,
		QueuedTasks:    queued,
		RunningTasks:   running,
	}, nil
}

// HandleEnqueue inserts a task via the FTS-dedup path.
func (h *handler) HandleEnqueue(ctx context.Context, args ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	if h.sup.db == nil {
		return ipc.EnqueueReply{}, errDBClosed
	}
	taskID := args.TaskID
	if taskID == "" {
		taskID = uuid.NewString()
	}
	taskID, inserted, err := h.sup.db.EnqueueTask(ctx,
		taskID, args.Description, args.Priority)
	if err != nil {
		return ipc.EnqueueReply{}, err
	}
	return ipc.EnqueueReply{TaskID: taskID, Inserted: inserted}, nil
}

// HandleStop requests a graceful shutdown. The IPC server closes the
// socket after replying; Run() unblocks and runs gracefulShutdown.
func (h *handler) HandleStop(_ context.Context, _ ipc.StopArgs) error {
	h.sup.Shutdown()
	return nil
}

// HandleReloadConfig is a stub in M2 — fills in when config reloading
// is wired through the session pool in M3. Supervisors that don't
// know how to reload yet refuse politely rather than lie.
func (h *handler) HandleReloadConfig(_ context.Context) error {
	return errReloadNotImplemented
}

// HandleAttach streams recent events from the supervisor. M2
// implementation is deliberately minimal: tails the event log from
// the last known ID every 500ms. M3 replaces this with a proper
// in-memory fan-out from the session pool so per-second events don't
// require a DB round-trip.
func (h *handler) HandleAttach(ctx context.Context, emit func(json.RawMessage) error) error {
	if h.sup.db == nil {
		return errDBClosed
	}
	var lastID int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var emitErr error
			err := h.sup.db.Replay(ctx, lastID, func(e db.Event) error {
				lastID = e.ID
				raw, mErr := json.Marshal(map[string]any{
					"id":        e.ID,
					"timestamp": e.Timestamp,
					"stream":    e.Stream,
					"kind":      e.Kind,
					"actor":     e.Actor,
					"payload":   json.RawMessage(e.PayloadRaw),
				})
				if mErr != nil {
					return mErr
				}
				return emit(raw)
			})
			if err != nil {
				emitErr = err
			}
			if emitErr != nil {
				return emitErr
			}
		}
	}
}
