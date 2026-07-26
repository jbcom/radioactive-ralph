package supervisor

import (
	"context"
	"errors"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// HandleObserveSnapshot serves the v3 transport-neutral read model from the
// supervisor-owned store. It has no direct-store fallback.
func (s *Supervisor) HandleObserveSnapshot(
	ctx context.Context,
	args ipc.ObserveSnapshotArgs,
) (*ipc.ObserveSnapshotReply, error) {
	service, err := observe.New(s.store)
	if err != nil {
		return nil, err
	}
	reply, err := service.Snapshot(ctx, args)
	if err != nil {
		return nil, codeObserveError(err)
	}
	return reply, nil
}

// HandleObserveMessages serves bounded content-free A2A message metadata from
// the same read-only service.
func (s *Supervisor) HandleObserveMessages(
	ctx context.Context,
	args ipc.ObserveMessagesArgs,
) (*ipc.ObserveMessagesReply, error) {
	service, err := observe.New(s.store)
	if err != nil {
		return nil, err
	}
	reply, err := service.Messages(ctx, args)
	if err != nil {
		return nil, codeObserveError(err)
	}
	return reply, nil
}

type observeCodedError struct {
	err  error
	code string
}

func (e *observeCodedError) Error() string { return e.err.Error() }
func (e *observeCodedError) Unwrap() error { return e.err }
func (e *observeCodedError) Code() string  { return e.code }

func codeObserveError(err error) error {
	switch {
	case errors.Is(err, store.ErrOperatorInvalidQuery),
		errors.Is(err, store.ErrOperatorInvalidCursor):
		return &observeCodedError{err: err, code: ipc.CodeInvalidArgs}
	case errors.Is(err, store.ErrOperatorProjectNotFound),
		errors.Is(err, store.ErrOperatorPlanNotFound),
		errors.Is(err, store.ErrOperatorTaskNotFound):
		return &observeCodedError{err: err, code: ipc.CodeNotFound}
	default:
		return err
	}
}

var _ ipc.QueryHandler = (*Supervisor)(nil)
