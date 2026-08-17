//go:build !darwin && !linux && !windows

package adapters

import (
	"fmt"
	"os"
	"os/exec"
)

func managedOpenCodeProviderSupported() bool { return false }

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func configureOpenCodeProviderProcess(*exec.Cmd) {}

func waitOpenCodeProviderExit(*os.Process) error {
	return fmt.Errorf("managed OpenCode providers are unsupported")
}

func reclaimOpenCodeProviderProcess(*os.Process, string) error {
	return fmt.Errorf("managed OpenCode process-group cleanup is unsupported")
}

func providerExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
