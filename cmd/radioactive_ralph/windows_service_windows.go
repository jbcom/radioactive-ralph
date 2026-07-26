//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/service"
	"golang.org/x/sys/windows/svc"
)

// windowsSCMProcessGuardExitCode is deliberately distinct from Cobra's
// ordinary command failure (1), so native SCM process-trace evidence can prove
// the pre-Cobra service guard was the component that rejected execution.
const windowsSCMProcessGuardExitCode = 78

// detectWindowsService is replaceable only so the Windows unit test can prove
// the process-entry guard without requiring a real SCM launch.
var detectWindowsService = svc.IsWindowsService

// maybeRunWindowsService intercepts an SCM launch before Cobra, configuration
// loading, environment changes, or supervisor startup. Registrations created
// by older development builds therefore fail closed even if their command line
// still names a legacy --windows-service-config payload. An operator-attached
// `radioactive_ralph --supervisor` is not an SCM process and remains on the
// ordinary foreground path.
func maybeRunWindowsService() (handled bool, exitCode int) {
	isService, err := detectWindowsService()
	if err != nil {
		// An uncertain context must not fall through to Cobra: a legacy SCM
		// registration could otherwise continue into --supervisor as SYSTEM.
		disabled := service.NewWindowsSCMDisabledError(service.WindowsSCMOperationExecute)
		fmt.Fprintf(os.Stderr, "radioactive_ralph: cannot verify foreground process context: %v; %v\n", err, disabled)
		return true, windowsSCMProcessGuardExitCode
	}
	if !isService {
		return false, 0
	}

	disabled := service.NewWindowsSCMDisabledError(service.WindowsSCMOperationExecute)
	fmt.Fprintf(os.Stderr, "radioactive_ralph: %v\n", disabled)
	return true, windowsSCMProcessGuardExitCode
}
