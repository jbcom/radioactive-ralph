//go:build darwin || linux

package adapters

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func managedOpenCodeProviderSupported() bool { return true }

func configureOpenCodeProviderProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return killOpenCodeProviderGroup(cmd.Process) }
	cmd.WaitDelay = openCodeProviderReapTimeout
}

func killOpenCodeProviderGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	if process.Pid <= 1 {
		return fmt.Errorf("invalid provider process group")
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// reclaimOpenCodeProviderProcess proves that the separately created provider
// process group has no surviving descendant before final Stop verification.
func reclaimOpenCodeProviderProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := killOpenCodeProviderGroup(process); err != nil {
		return fmt.Errorf("reap provider process group: %w", err)
	}
	deadline := time.Now().Add(openCodeProviderReapTimeout)
	for {
		live, err := openCodeProviderGroupLive(process.Pid)
		if err != nil {
			return fmt.Errorf("prove provider process group absent: %w", err)
		}
		if !live {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("provider process group remained live after cleanup")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func providerExitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	if code := err.ExitCode(); code >= 0 {
		return code
	}
	return 1
}
