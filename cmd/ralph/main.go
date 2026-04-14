// Command ralph is the radioactive-ralph CLI entry point.
//
// Ralph is a per-repo meta-orchestrator that keeps a fleet of Claude
// subprocesses alive, focused, and productive across days of autonomous
// development work. See https://github.com/jbcom/radioactive-ralph for
// the full rationale and architecture plan.
package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
)

// Version is set by GoReleaser at build time via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// cli is the top-level kong structure. Subcommands are added in M2+
// as each layer of the daemon comes online.
type cli struct {
	Version kong.VersionFlag `help:"Print version and exit."`
}

func main() {
	var c cli
	ctx := kong.Parse(&c,
		kong.Name("ralph"),
		kong.Description("Autonomous continuous development orchestrator for Claude Code."),
		kong.Vars{"version": fmt.Sprintf("%s (%s, built %s)", Version, Commit, Date)},
		kong.UsageOnError(),
	)
	_ = ctx
	// Subcommands will wire through kong.Run(&c, ...) once they exist.
	// For M2.T1 bootstrap, only --version works; the CLI exits cleanly
	// with a help message otherwise.
	fmt.Fprintln(os.Stderr, "ralph: no subcommand given (M2 in progress)")
	os.Exit(64) // EX_USAGE
}
