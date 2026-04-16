package runtime

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

var errEventDBClosed = errors.New("runtime: event log closed")

type handler struct {
	svc *Service
}

func (h *handler) HandleStatus(ctx context.Context) (ipc.StatusReply, error) {
	return h.svc.status(ctx)
}

func (h *handler) HandleEnqueue(ctx context.Context, args ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	if h.svc.eventDB == nil {
		return ipc.EnqueueReply{}, errEventDBClosed
	}
	taskID := args.TaskID
	if taskID == "" {
		taskID = uuid.NewString()
	}
	taskID, inserted, err := h.svc.eventDB.EnqueueTask(ctx, taskID, args.Description, args.Priority)
	if err != nil {
		return ipc.EnqueueReply{}, err
	}
	return ipc.EnqueueReply{TaskID: taskID, Inserted: inserted}, nil
}

func (h *handler) HandleStop(_ context.Context, _ ipc.StopArgs) error {
	h.svc.Shutdown()
	return nil
}

func (h *handler) HandleReloadConfig(_ context.Context) error {
	return nil
}

func (h *handler) HandleAttach(ctx context.Context, emit func(json.RawMessage) error) error {
	if h.svc.eventDB == nil {
		return errEventDBClosed
	}
	var lastID int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			err := h.svc.eventDB.Replay(ctx, lastID, func(e db.Event) error {
				lastID = e.ID
				raw, mErr := json.Marshal(map[string]any{
					"id":        e.ID,
					"timestamp": e.Timestamp,
					"stream":    e.Stream,
					"kind":      e.Kind,
					"actor":     e.Actor,
					"payload":   json.RawMessage(e.PayloadRaw),
					"pid":       os.Getpid(),
				})
				if mErr != nil {
					return mErr
				}
				return emit(raw)
			})
			if err != nil {
				return err
			}
		}
	}
}
