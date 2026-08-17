//go:build windows

package adapters

import (
	"fmt"
	"os"
	"os/exec"
)

func managedOpenCodeProviderSupported() bool { return false }

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func configureOpenCodeProviderProcess(cmd *exec.Cmd) {
	// Native Windows providers are disabled. Preserve CommandContext's stable
	// direct-process handle and bound any inherited I/O wait for compile parity.
	cmd.WaitDelay = openCodeProviderReapTimeout
}

func waitOpenCodeProviderExit(*os.Process) error {
	return fmt.Errorf("managed OpenCode providers are unsupported on native Windows")
}

func reclaimOpenCodeProviderProcess(*os.Process, string) error { return nil }

func providerExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
