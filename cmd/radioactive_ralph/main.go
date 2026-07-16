// Command radioactive_ralph is the one binary, two modes described in
// docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md §4:
//
//   - `radioactive_ralph --supervisor` runs the durable control-plane
//     process — pty ownership, IPC, the single user-level store, the
//     reaper. Working directory is irrelevant to it.
//   - Plain `radioactive_ralph` is a dumb client: it resolves the current
//     directory's project, finds the running supervisor (or tells the
//     operator how to start one), and talks to it. It owns no ptys, no DB,
//     no business logic of its own.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jbcom/radioactive-ralph/internal/vconfig"
	"github.com/spf13/cobra"
)

// Version, Commit, and Date are set by GoReleaser at build time via
// -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	os.Exit(run())
}

// run builds the root command and executes it, returning the process exit
// code. Extracted from main so signal-context cleanup always runs before
// the process actually exits.
func run() int {
	ctx, cancel := signalContext()
	defer cancel()

	root := newRootCmd(ctx)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: %v\n", err)
		return 1
	}
	return 0
}

// rootFlags collects the root-level flags that decide which of the two
// modes (§4) this invocation runs in.
type rootFlags struct {
	supervisor bool
	initFlag   bool
}

func newRootCmd(ctx context.Context) *cobra.Command {
	var flags rootFlags

	root := &cobra.Command{
		Use:     "radioactive_ralph",
		Short:   "Repo-scoped runtime for AI-assisted software work",
		Version: fmt.Sprintf("%s (%s, built %s)", Version, Commit, Date),
		// SilenceUsage: operational errors (no supervisor running, project
		// not initialized, etc.) are expected-path outcomes, not usage
		// mistakes; printing the full usage block on every one is noise.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return dispatchRoot(cmd.Context(), cmd, flags)
		},
	}
	root.SetContext(ctx)

	// The three virtual-config-layer flags (spec §5a) are shared between
	// supervisor and client invocations; AddFlags registers them as
	// persistent so every subcommand sees the same three names.
	vconfig.AddFlags(root)

	root.Flags().BoolVar(&flags.supervisor, "supervisor", false,
		"run as the durable supervisor process (spec §4); working directory is irrelevant in this mode")
	root.Flags().BoolVar(&flags.initFlag, "init", false,
		"initialize (or re-initialize) the current directory as a known project")

	root.AddCommand(newDoctorCmd())

	return root
}

// dispatchRoot implements the mode switch described in spec §4: exactly
// one of --supervisor or the dumb-client path runs per invocation.
// --init is a client-side action (it always resolves against the current
// directory), so it composes with the plain-client path rather than being
// a third mode.
func dispatchRoot(ctx context.Context, cmd *cobra.Command, flags rootFlags) error {
	if flags.supervisor {
		return runSupervisorMode(ctx)
	}
	if flags.initFlag {
		return runInitMode(ctx, cmd)
	}
	return runClientMode(ctx, cmd)
}

// signalContext returns a context canceled on SIGINT/SIGTERM so the
// supervisor's Run loop (the only long-lived path) shuts down cleanly
// instead of leaving the socket/PID lock behind.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()
	return ctx, cancel
}
