// Command radioactive_ralph is the radioactive-ralph CLI entry point.
//
// Ralph is a per-repo runtime binary with multiple built-in
// personas. The runtime ships provider bindings for claude, codex, and
// gemini today, while keeping the product contract provider-oriented
// rather than provider-branded. See
// https://github.com/jbcom/radioactive-ralph for the full rationale and
// architecture plan.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
)

// Version is set by GoReleaser at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// cli is the top-level kong structure. Every subcommand lives in its
// own file to keep each one focused and under the 300-LOC limit.
type cli struct {
	Version kong.VersionFlag `help:"Print version and exit."`

	Init    InitCmd    `cmd:"" help:"Set up a fresh .radioactive-ralph/ tree in the current repo."`
	Run     RunCmd     `cmd:"" help:"Run one bounded variant attached to the current terminal."`
	Status  StatusCmd  `cmd:"" help:"Query the repo service over the local control plane."`
	Attach  AttachCmd  `cmd:"" help:"Stream the repo service event log."`
	Stop    StopCmd    `cmd:"" help:"Ask the running repo service to shut down gracefully."`
	Doctor  DoctorCmd  `cmd:"" help:"Run environment health checks."`
	Service ServiceCmd `cmd:"" help:"Manage OS service units for the durable repo runtime."`
	TUI     TUICmd     `cmd:"" help:"Open the repo service cockpit."`
	Plan    PlanCmd    `cmd:"" help:"Query + manage plans in the plan DAG."`
}

func main() {
	os.Exit(mainCode())
}

// mainCode runs the CLI and returns the exit code so deferred cleanup
// (signal context cancel) always runs before process exit.
func mainCode() int {
	var c cli
	kctx := kong.Parse(&c,
		kong.Name("radioactive_ralph"),
		kong.Description("Helpful little repo runtime with built-in Ralph personas."),
		kong.Vars{"version": fmt.Sprintf("%s (%s, built %s)", Version, Commit, Date)},
		kong.UsageOnError(),
	)

	ctx, cancel := signalContext()
	defer cancel()

	if err := kctx.Run(&runContext{ctx: ctx}); err != nil {
		fmt.Fprintf(os.Stderr, "radioactive_ralph: %v\n", err)
		return 1
	}
	return 0
}

// runContext is the shared context-carrier passed to every Run method
// so subcommands can see the same cancellation channel.
type runContext struct {
	ctx context.Context
}

// signalContext returns a context canceled on SIGINT/SIGTERM. Long-
// running subcommands (Run, Attach, Supervisor) honor it for graceful
// shutdown; short-lived subcommands (Status, Stop, Doctor) don't care
// and finish before the signal would fire anyway.
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
