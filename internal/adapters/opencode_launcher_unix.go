//go:build !windows

package adapters

import (
	"os/exec"
	"syscall"
)

func providerExitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
