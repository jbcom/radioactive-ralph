//go:build !windows

package provider

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroupKill configures cmd so that ctx cancellation (or an explicit
// kill) reaps the WHOLE process group, not just the direct child. Declarative
// providers run a batch CLI (often a shell wrapper) with exec.CommandContext;
// that CLI can fork grandchildren (a `sleep`, a git subprocess, a tool) which
// INHERIT the stdout pipe. exec.CommandContext's default cancel only kills the
// direct child, so a surviving grandchild keeps the pipe open and the reader
// blocks PAST the turn timeout — defeating the never-block bound. Setpgid makes
// the child a group leader; Cancel signals the negative PID to SIGKILL the whole
// group; WaitDelay bounds the wait for stdout copying to finish after the kill.
func setProcessGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill() // fall back to the direct child
		}
		return nil
	}
	// After Cancel fires, don't wait forever for pipe copies to drain — force the
	// I/O to unblock so Run returns bounded even if a grandchild lingers a moment.
	cmd.WaitDelay = 5 * time.Second
}
