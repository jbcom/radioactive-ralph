//go:build !gui

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

// newGUICmd is the stub for builds compiled WITHOUT `-tags gui`. The Fyne GUI
// is a CGO dependency excluded from the default cross-platform build, so the
// command still exists (so `--help` lists it consistently) but explains how to
// get a GUI-capable binary and exits nonzero rather than pretending to launch.
func newGUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gui",
		Short: "Open the Ralph desktop client (requires a GUI-enabled build)",
		Long: "The desktop client is not compiled into this binary. Rebuild with " +
			"the `gui` build tag (go build -tags gui) or install a GUI-enabled " +
			"release to use it. The terminal UI (run radioactive_ralph with no " +
			"subcommand) offers the same live view.",
		RunE: func(*cobra.Command, []string) error {
			return errors.New("this build has no GUI support — rebuild with `-tags gui` or install a GUI-enabled release")
		},
	}
}
