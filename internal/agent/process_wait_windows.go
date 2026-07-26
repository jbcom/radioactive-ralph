//go:build windows

package agent

import (
	"errors"
	"os/exec"
	"syscall"
)

// Windows reports TerminateProcess through an exit code rather than a Unix
// signal status. os.Process.Kill uses exit code 1, so require both a requested
// termination and that concrete wait status. Plain or otherwise unclassified
// wait errors remain visible instead of being suppressed as forced exits.
func processWaitWasForced(waitErr error, terminationRequested bool) bool {
	if !terminationRequested {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ProcessState == nil {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.ExitCode == 1
}
