//go:build linux

package proclife

import (
	"os/exec"
	"syscall"
)

// attach sets Pdeathsig on the command so the kernel signals the
// child with SIGTERM when the parent process exits for any reason.
// Works even if the parent dies by SIGKILL — the lifeline pipe
// can't cover that path on Linux, but Pdeathsig can.
func attach(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
	return nil
}

// postStart is a no-op on Linux. Pdeathsig was set before Start().
func postStart(_ *exec.Cmd) error {
	return nil
}
