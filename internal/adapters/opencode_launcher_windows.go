//go:build windows

package adapters

import (
	"os"
	"os/exec"
	"time"
)

func managedOpenCodeProviderSupported() bool { return false }

func configureOpenCodeProviderProcess(cmd *exec.Cmd) {
	// Native Windows providers are disabled. Preserve CommandContext's stable
	// direct-process handle and bound any inherited I/O wait for compile parity.
	cmd.WaitDelay = 5 * time.Second
}

func reclaimOpenCodeProviderProcess(*os.Process) error { return nil }

func providerExitCode(err *exec.ExitError) int {
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
