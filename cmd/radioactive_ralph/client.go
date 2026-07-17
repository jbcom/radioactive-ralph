package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/tui"
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
// Once connected, this launches the read-only Bubble Tea TUI (spec §7) --
// "running the client simply shows the supervisor's live state." A
// non-terminal stdout (a pipe, a CI job, `go test`) NEVER launches the
// TUI: Bubble Tea would block forever reading/writing a stream that has
// no interactive end, so runClientMode falls back to the one-line status
// print that predates this phase.
func runClientMode(ctx context.Context, cmd *cobra.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}

	projectID, err := ensureProjectKnown(ctx, cmd, stateRoot, cwd)
	if err != nil {
		return err
	}

	// Fail fast with a clear, actionable message if no supervisor is
	// listening at all, before opening the store or launching the TUI.
	// This connection is used ONLY for that check (and, on the non-tty
	// path, the one status print) — it is NOT held open across the TUI's
	// lifetime: ipc.Client connections are one-shot (see
	// internal/tui/live.go's liveDataSource doc comment), so a live TUI
	// session redials per call instead of reusing this one.
	client, err := supervisor.Find(stateRoot)
	if err != nil {
		if errors.Is(err, supervisor.ErrNoSupervisor) {
			// No supervisor. On an interactive terminal, OFFER to set one up
			// (guided, consent-gated). On a non-interactive stdin/stdout
			// (pipe, CI, go test) the wizard is skipped and we keep the exact
			// print-commands-and-exit-nonzero behavior the tests assert.
			if onboardingInteractive() {
				reready, oerr := runFirstRunWizard(stateRoot)
				if oerr != nil {
					return oerr
				}
				if reready {
					// A supervisor is now up — re-discover and continue to the TUI.
					client, err = supervisor.Find(stateRoot)
				}
				if !reready || err != nil {
					// User chose foreground/manual, or re-discovery failed.
					return errNoSupervisorListening
				}
			} else {
				fmt.Fprintln(os.Stderr, noSupervisorMessage)
				return errNoSupervisorListening
			}
		} else {
			return fmt.Errorf("find supervisor: %w", err)
		}
	}

	if !tui.IsTerminal() {
		defer func() { _ = client.Close() }()
		return printStatus(ctx, client)
	}
	_ = client.Close()

	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	source := tui.NewLiveDataSource(stateRoot, st, projectID)
	return tui.Run(ctx, source, tui.Options{ProjectID: projectID})
}

// printStatus is the non-tty fallback: a single status line, no
// interactive program. This is what ran unconditionally before this
// phase; it now also serves as the guard against launching the TUI when
// stdout isn't a terminal (a pipe, a CI job, `go test`), so those paths
// never block on a Bubble Tea program that has no interactive end to
// drive it.
func printStatus(ctx context.Context, client *ipc.Client) error {
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := client.Status(statusCtx)
	if err != nil {
		return fmt.Errorf("supervisor status: %w", err)
	}

	fmt.Printf("radioactive_ralph: supervisor is up (pid %d, uptime %s, %d active worker(s))\n",
		status.PID, status.Uptime.Round(time.Second), status.ActiveWorkers)
	return nil
}

// ensureProjectKnown resolves cwd against the store's accumulated
// fingerprints (spec §5b) and, when the directory is not yet a known
// project, transparently runs the same headless init path as an explicit
// --init rather than failing the plain-client invocation outright. It
// returns the resolved project ID either way so callers can scope
// project-level reads (e.g. the TUI's plan list) without a second
// fingerprint resolution.
func ensureProjectKnown(ctx context.Context, cmd *cobra.Command, stateRoot, cwd string) (string, error) {
	dbPath := storeDBPath(stateRoot)
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	fps, err := store.Fingerprints(ctx, cwd)
	if err != nil {
		return "", fmt.Errorf("compute project fingerprints: %w", err)
	}

	projectID, found, err := st.ResolveProject(ctx, fps)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	if found {
		if err := st.TouchProjectLastSeen(ctx, projectID); err != nil {
			return "", fmt.Errorf("touch project: %w", err)
		}
		return projectID, nil
	}

	fmt.Fprintln(os.Stderr, "radioactive_ralph: this directory is not yet a known project; running init...")
	if err := runInitMode(ctx, cmd); err != nil {
		return "", err
	}

	// runInitMode created the project against its own store handle;
	// re-resolve against the same fingerprints to get the ID without
	// runInitMode needing to change its signature/return value.
	projectID, found, err = st.ResolveProject(ctx, fps)
	if err != nil {
		return "", fmt.Errorf("resolve project after init: %w", err)
	}
	if !found {
		return "", fmt.Errorf("project not found immediately after init")
	}
	return projectID, nil
}
