package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// runClientMode implements the dumb-client half of spec §4: it resolves
// the current directory to a known project, and if the project is
// unknown auto-routes to the same headless init path as an explicit
// --init (spec §4: "if the directory is not yet a known project in the
// user DB, it auto-routes to init"). Once a project is confirmed it
// tries to Find a live supervisor; if none answers it prints a clear,
// actionable message rather than hanging or silently no-opping (spec §4:
// "it refuses to run unless a supervisor is listening (offering to start
// one)").
//
// The full read-only TUI (spec §7) is Phase 7; today a successful connect
// just prints the supervisor's status so this path is observably useful
// on its own.
func runClientMode(ctx context.Context, cmd *cobra.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}

	if err := ensureProjectKnown(ctx, cmd, stateRoot, cwd); err != nil {
		return err
	}

	client, err := supervisor.Find(stateRoot)
	if err != nil {
		if errors.Is(err, supervisor.ErrNoSupervisor) {
			fmt.Fprintln(os.Stderr, "radioactive_ralph: no supervisor is running.")
			fmt.Fprintln(os.Stderr, "Start one with:  radioactive_ralph --supervisor")
			return fmt.Errorf("no supervisor listening")
		}
		return fmt.Errorf("find supervisor: %w", err)
	}
	defer func() { _ = client.Close() }()

	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := client.Status(statusCtx)
	if err != nil {
		return fmt.Errorf("supervisor status: %w", err)
	}

	fmt.Printf("radioactive_ralph: supervisor is up (pid %d, uptime %s, %d active worker(s))\n",
		status.PID, status.Uptime.Round(time.Second), status.ActiveWorkers)
	fmt.Println("The interactive cockpit (radioactive_ralph tui) lands in a later phase; this is a status probe for now.")
	return nil
}

// ensureProjectKnown resolves cwd against the store's accumulated
// fingerprints (spec §5b) and, when the directory is not yet a known
// project, transparently runs the same headless init path as an explicit
// --init rather than failing the plain-client invocation outright.
func ensureProjectKnown(ctx context.Context, cmd *cobra.Command, stateRoot, cwd string) error {
	dbPath := storeDBPath(stateRoot)
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	fps, err := store.Fingerprints(ctx, cwd)
	if err != nil {
		return fmt.Errorf("compute project fingerprints: %w", err)
	}

	projectID, found, err := st.ResolveProject(ctx, fps)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	if found {
		return st.TouchProjectLastSeen(ctx, projectID)
	}

	fmt.Fprintln(os.Stderr, "radioactive_ralph: this directory is not yet a known project; running init...")
	return runInitMode(ctx, cmd)
}
