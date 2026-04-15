package main

import (
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/doctor"
)

// DoctorCmd is `radioactive_ralph doctor`.
type DoctorCmd struct{}

// Run prints the doctor report.
func (c *DoctorCmd) Run(rc *runContext) error {
	report := doctor.Run(rc.ctx)
	report.WriteText(os.Stdout)
	if !report.Passed() {
		return fmt.Errorf("doctor: %d check(s) failed", report.FailCount)
	}
	return nil
}
