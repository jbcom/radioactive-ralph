package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/projectid"
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

	// Project identity is resolved AFTER supervisor discovery, not before.
	// project-ensure is a supervisor drive command, and the first-run wizard
	// below exists precisely for the operator who has no supervisor yet —
	// resolving identity first would fail before the wizard could offer to
	// install one, breaking the cold-start experience it provides.
	//
	// Fail fast with a clear, actionable message if no supervisor is
	// listening at all, before opening the store or launching the TUI.
	// This connection is used ONLY for that check (and, on the non-tty
	// path, the one status print) — it is NOT held open across the TUI's
	// lifetime: ipc.Client connections are one-shot (see
	// internal/tui/live.go's liveDataSource doc comment), so a live TUI
	// session redials per call instead of reusing this one.
	client, err := supervisor.Find(stateRoot)
	if err != nil {
		if !errors.Is(err, supervisor.ErrNoSupervisor) {
			return fmt.Errorf("find supervisor: %w", err)
		}
		// No supervisor. On a non-interactive stdin/stdout (pipe, CI, go test)
		// the wizard is skipped: keep the exact print-commands-and-exit-nonzero
		// behavior the tests assert.
		if !onboardingInteractive() {
			fmt.Fprintln(os.Stderr, noSupervisorMessage())
			return errNoSupervisorListening
		}
		// Interactive: OFFER to set one up (guided, consent-gated).
		reready, oerr := runFirstRunWizard(ctx, stateRoot)
		if oerr != nil {
			return oerr
		}
		if !reready {
			// User chose the foreground/manual path.
			return errNoSupervisorListening
		}
		// A supervisor should now be up — re-discover it. A nil/failed
		// re-Find here means it didn't actually come up in time; surface the
		// same no-supervisor error rather than proceeding with a nil client.
		client, err = supervisor.Find(stateRoot)
		if err != nil {
			return errNoSupervisorListening
		}
	}

	projectID, err := ensureProjectKnown(ctx, cmd, stateRoot, cwd)
	if err != nil {
		_ = client.Close()
		return err
	}

	if !tui.IsTerminal() {
		defer func() { _ = client.Close() }()
		return printStatus(ctx, client, projectID)
	}
	_ = client.Close()

	source := tui.NewLiveDataSource(stateRoot, projectID)
	return tui.Run(ctx, source, tui.Options{ProjectID: projectID})
}

// printStatus is the non-tty fallback: a single status line, no
// interactive program. This is what ran unconditionally before this
// phase; it now also serves as the guard against launching the TUI when
// stdout isn't a terminal (a pipe, a CI job, `go test`), so those paths
// never block on a Bubble Tea program that has no interactive end to
// drive it.
func printStatus(ctx context.Context, client *ipc.Client, projectID string) error {
	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return runStatusQueryWith(
		statusCtx,
		os.Stdout,
		client,
		ipc.ObserveSnapshotArgs{ProjectID: projectID},
		false,
		false,
	)
}

// ensureProjectKnown resolves cwd against the store's accumulated
// fingerprints (spec §5b) and, when the directory is not yet a known
// project, transparently runs the same headless init path as an explicit
// --init rather than failing the plain-client invocation outright. It
// returns the resolved project ID either way so callers can scope
// project-level reads (e.g. the TUI's plan list) without a second
// fingerprint resolution.
func ensureProjectKnown(ctx context.Context, _ *cobra.Command, stateRoot, cwd string) (string, error) {
	// Identity signals come from the caller's OWN working directory — an
	// absolute path plus two git facts. That is a local computation, not a
	// store read, so the client may derive it (internal/projectid). What it
	// must not do is open the database to resolve them: the supervisor is the
	// single writer of record, and project-ensure is a write.
	computed, err := projectid.Compute(ctx, cwd)
	if err != nil {
		return "", fmt.Errorf("compute project fingerprints: %w", err)
	}
	fps := make([]ipc.ProjectFingerprint, 0, len(computed))
	for _, fp := range computed {
		fps = append(fps, ipc.ProjectFingerprint{Kind: fp.Kind, Value: fp.Value})
	}

	client, err := supervisor.Find(stateRoot)
	if err != nil {
		return "", fmt.Errorf(
			"%w: identifying this project needs a running supervisor; start one with: %s",
			errNoSupervisorListening, supervisorStartHint())
	}
	defer func() { _ = client.Close() }()

	reply, err := client.ProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		Fingerprints: fps,
		DisplayName:  filepath.Base(cwd),
	})
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	if reply.Created {
		fmt.Fprintf(os.Stderr,
			"radioactive_ralph: this directory is not yet a known project; registered it as %s\n",
			reply.ProjectID)
	}
	return reply.ProjectID, nil
}
