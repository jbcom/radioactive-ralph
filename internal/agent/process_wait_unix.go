//go:build !windows

package agent

import (
	"errors"
	"os/exec"
	"syscall"
)

// processWaitWasForced makes the final natural/forced classification from the
// status actually returned by cmd.Wait. A successful signal request that lost
// a probe-to-signal race to a normal exit remains a natural exit.
func processWaitWasForced(waitErr error, terminationRequested bool) bool {
	if !terminationRequested {
		return false
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ProcessState == nil {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled()
}
