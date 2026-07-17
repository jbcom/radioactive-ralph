//go:build gui

package main

import (
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/gui"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// newGUICmd builds the `gui` subcommand: the Fyne desktop client, a peer to the
// TUI on the same supervisor socket. Present only in builds compiled with
// `-tags gui` (Fyne is a CGO dependency; the default build ships a stub — see
// gui_cmd_stub.go). It opens the window even when no supervisor is reachable
// yet: the window polls Status on its refresh tick and lights up when one
// appears, which is friendlier than refusing to launch.
func newGUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gui",
		Short: "Open the Ralph desktop client (Fyne)",
		Long: "Launches the radioactive-ralph desktop application: a graphical " +
			"peer to the terminal UI that watches AND drives the supervisor " +
			"(approve/pause/kill/import) over the same local socket.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
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
			st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer func() { _ = st.Close() }()

			ctrl := gui.NewLiveController(stateRoot, st, projectID)
			return gui.Run(ctx, gui.Opts{Controller: ctrl, ProjectID: projectID})
		},
	}
}
