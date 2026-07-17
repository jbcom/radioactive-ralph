//go:build gui

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/gui"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// maybeLaunchDesktopGUI opens the desktop GUI when the binary was launched from
// a desktop context rather than a terminal — i.e. neither stdin NOR stdout is a
// TTY, which is exactly how a double-clicked .app / AppImage / .exe starts (no
// controlling terminal). In that case the read-only TUI would have nothing to
// draw into, so the GUI is the right surface. A bare launch from an actual
// terminal keeps both TTYs and falls through (handled=false) to the client
// path, preserving the CLI's existing behavior.
func maybeLaunchDesktopGUI(ctx context.Context, cmd *cobra.Command) (handled bool, err error) {
	stdinTTY := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	stdoutTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	if stdinTTY || stdoutTTY {
		return false, nil // launched from a terminal — use the CLI/TUI path
	}

	cwd, err := os.Getwd()
	if err != nil {
		return true, fmt.Errorf("resolve cwd: %w", err)
	}
	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return true, fmt.Errorf("resolve state root: %w", err)
	}
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return true, fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	// A desktop launch (Finder / Explorer / a file manager) has an arbitrary
	// working directory — usually NOT a repo. So resolve the project
	// NON-MUTATINGLY: if the launch dir happens to be a known project, scope the
	// GUI to it; otherwise open project-agnostic (empty id → the GUI lists every
	// project). We must NOT auto-init the launch directory the way the CLI path
	// does — that would register "/" (or wherever Finder launched us) as a
	// bogus project.
	projectID := ""
	if fps, ferr := store.Fingerprints(ctx, cwd); ferr == nil {
		if id, found, rerr := st.ResolveProject(ctx, fps); rerr == nil && found {
			projectID = id
		}
	}

	ctrl := gui.NewLiveController(stateRoot, st, projectID)
	return true, gui.Run(ctx, gui.Opts{Controller: ctrl, ProjectID: projectID})
}
