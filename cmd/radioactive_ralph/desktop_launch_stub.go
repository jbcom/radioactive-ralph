//go:build !gui

package main

import (
	"context"

	"github.com/spf13/cobra"
)

// maybeLaunchDesktopGUI is a no-op in the default (non-GUI) build: there is no
// GUI to launch, so a bare invocation always falls through to the client path.
func maybeLaunchDesktopGUI(context.Context, *cobra.Command) (handled bool, err error) {
	return false, nil
}
