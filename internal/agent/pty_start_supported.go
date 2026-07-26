//go:build darwin || linux

package agent

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

func startPTY(cmd *exec.Cmd, oneShotInput bool) (*os.File, error) {
	if !oneShotInput {
		return pty.Start(cmd)
	}

	attrs := cmd.SysProcAttr
	if attrs == nil {
		attrs = &syscall.SysProcAttr{}
	}
	attrs.Setsid = true
	attrs.Setctty = true
	// stdin is the dedicated one-shot pipe, so stdout is the first child file
	// descriptor backed by the slave PTY and must become the controlling tty.
	attrs.Ctty = 1
	return pty.StartWithAttrs(cmd, nil, attrs)
}
