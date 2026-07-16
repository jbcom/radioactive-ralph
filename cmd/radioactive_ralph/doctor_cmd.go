package main

import (
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/doctor"
	"github.com/spf13/cobra"
)

// newDoctorCmd wires the standalone internal/doctor environment-health
// checks (git/claude/codex/gh versions + auth) as a cobra subcommand.
// doctor has no dependency on the old per-repo config/variant model being
// retired in this phase, so it ports over unchanged.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "doctor",
		Short:        "Run environment health checks",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := doctor.Run(cmd.Context())
			report.WriteText(os.Stdout)
			if !report.Passed() {
				return fmt.Errorf("doctor: %d check(s) failed", report.FailCount)
			}
			return nil
		},
	}
}
