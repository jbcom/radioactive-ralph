//go:build gui

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/gui"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/projectid"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
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
func maybeLaunchDesktopGUI(ctx context.Context, _ *cobra.Command) (handled bool, err error) {
	stdinTTY := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	stdoutTTY := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	if !shouldLaunchDesktopGUI(stdinTTY, stdoutTTY) {
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
	// A desktop launch (Finder / Explorer / a file manager) has an arbitrary
	// working directory — usually NOT a repo, sometimes "/". So resolve the
	// project NON-MUTATINGLY via ResolveOnly: if the launch dir happens to be a
	// known project, scope the GUI to it; otherwise leave the scope empty and
	// let the safe supervisor query fail closed with an actionable banner.
	//
	// We must NOT auto-init the way the CLI path does — that would register
	// wherever Finder launched us as a durable, operator-visible project just
	// because someone double-clicked an icon.
	//
	// Resolution goes through the SUPERVISOR rather than the store: this was
	// the last CLI file opening the database directly, which made the client a
	// second writer to a supervisor-owned DB.
	projectID := ""
	if client, ferr := supervisor.Find(stateRoot); ferr == nil {
		computed, cerr := projectid.Compute(ctx, cwd)
		if cerr == nil {
			fps := make([]ipc.ProjectFingerprint, 0, len(computed))
			for _, fp := range computed {
				fps = append(fps, ipc.ProjectFingerprint{Kind: fp.Kind, Value: fp.Value})
			}
			if reply, rerr := client.ProjectEnsure(ctx, ipc.ProjectEnsureArgs{
				ResolveOnly:  true,
				Fingerprints: fps,
			}); rerr == nil {
				projectID = reply.ProjectID
			}
		}
		_ = client.Close()
	}
	// Every failure above is deliberately non-fatal: no supervisor, an
	// unreadable cwd, or an unknown directory all mean "no project scope", and
	// the GUI already renders that state with a banner telling the operator
	// what to do. Refusing to launch would leave a double-clicked icon doing
	// nothing at all.

	ctrl := gui.NewLiveController(stateRoot, projectID)
	return true, gui.Run(ctx, gui.Opts{Controller: ctrl, ProjectID: projectID})
}

// shouldLaunchDesktopGUI is the complete production auto-launch predicate.
// Cobra calls maybeLaunchDesktopGUI only for the bare root command, so the only
// remaining signal is the controlling-terminal state: exactly as before, the
// GUI handles the invocation only when neither stdin nor stdout is a TTY.
func shouldLaunchDesktopGUI(stdinTTY, stdoutTTY bool) bool {
	return !stdinTTY && !stdoutTTY
}
