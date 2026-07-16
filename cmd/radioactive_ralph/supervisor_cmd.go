package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

// runSupervisorMode opens the single user-level store (spec §6) and runs
// the supervisor until ctx is cancelled or a client asks it to stop. The
// working directory is irrelevant here — everything is keyed off the XDG
// state root, never the caller's cwd (spec §4).
func runSupervisorMode(ctx context.Context) error {
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

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	fmt.Fprintf(os.Stderr, "radioactive_ralph: supervisor starting (state root %s)\n", stateRoot)
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
