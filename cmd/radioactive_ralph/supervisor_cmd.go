package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/rlog"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

// runSupervisorMode opens the single user-level store (spec §6) and runs
// the supervisor until ctx is cancelled or a client asks it to stop. The
// working directory is irrelevant here — everything is keyed off the XDG
// state root, never the caller's cwd (spec §4).
//
// logFormat selects internal/rlog's output shape ("text" or "json"):
// structured JSON logging matters here because the supervisor is the one
// long-lived process an operator or the E2E harness needs to observe from
// outside (tailing stderr, grepping for a lifecycle event) — a stream-json
// line per lifecycle/reaper event is far easier to assert on than an
// ad-hoc fmt.Fprintf line shape.
func runSupervisorMode(ctx context.Context, logFormat string) error {
	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return fmt.Errorf("create state root: %w", err)
	}

	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	mode := rlog.ModeText
	if logFormat == "json" {
		mode = rlog.ModeJSON
	}
	logger := rlog.New(mode, os.Stderr)

	logger.Info("supervisor.starting", "state_root", stateRoot)
	err = supervisor.Run(ctx, supervisor.Options{
		RuntimeDir: stateRoot,
		Store:      st,
		Logger: func(msg string, args ...any) {
			logger.Info(msg, args...)
		},
	})
	// supervisor.Run's errors already carry the "supervisor:" prefix; don't
	// double it.
	return err
}
