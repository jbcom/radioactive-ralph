//go:build !darwin && !linux && !windows

package adapters

import (
	"fmt"
	"os"
	"os/exec"
)

func managedOpenCodeProviderSupported() bool { return false }

func configureOpenCodeProviderProcess(*exec.Cmd) {}

func reclaimOpenCodeProviderProcess(*os.Process) error {
	return fmt.Errorf("managed OpenCode process-group cleanup is unsupported")
}

func providerExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
