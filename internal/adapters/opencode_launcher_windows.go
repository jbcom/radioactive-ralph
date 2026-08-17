//go:build windows

package adapters

import "os/exec"

func providerExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
