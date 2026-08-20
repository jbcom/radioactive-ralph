//go:build darwin || linux

package adapters

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

func managedOpenCodeProviderSupported() bool { return true }

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

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
func reclaimOpenCodeProviderProcess(process *os.Process, scopeValue string) error {
	if process == nil {
		return nil
	}
	live, err := openCodeProviderGroupLive(process.Pid)
	if err != nil {
		return fmt.Errorf("inspect provider process group before cleanup: %w", err)
	}
	if live {
		if err := killOpenCodeProviderGroup(process); err != nil {
			// Darwin returns EPERM when the group became zombie-only between the
			// liveness query and kill. Recheck while the waitable leader still
			// anchors the group identity; a remaining live member is an error.
			live, liveErr := openCodeProviderGroupLive(process.Pid)
			if liveErr != nil || live {
				return fmt.Errorf("reap provider process group: %w", err)
			}
		}
	}
	deadline := time.Now().Add(openCodeProviderReapTimeout)
	for {
		live, err := openCodeProviderGroupLive(process.Pid)
		if err != nil {
			return fmt.Errorf("prove provider process group absent: %w", err)
		}
		if !live {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("provider process group remained live after cleanup")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A tool can call setsid(2) or setpgid(2), leaving the provider's original
	// process group. Every managed descendant inherits a per-launch random
	// environment scope, so reclaim it through stable kernel process handles
	// before Stop can observe or accept checkout state.
	if err := agent.ReclaimProcessScope(
		openCodeProcessScopeEnv,
		scopeValue,
		process.Pid,
		openCodeProviderReapTimeout,
	); err != nil {
		return fmt.Errorf("reap escaped provider descendants: %w", err)
	}
	return nil
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
