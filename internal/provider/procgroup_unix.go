//go:build !windows

package provider

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// killProcessTree SIGKILLs p and its whole process group (Setpgid made the child
// a group leader, so PGID == PID). Used both by setProcessGroupKill's ctx-cancel
// hook and directly when we abandon a still-writing CLI (e.g. an oversized
// stream-json line) and must not block cmd.Wait() on a full pipe.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	// CRITICAL guard: Kill(-pid) with pid 0 signals the CALLER's own process group
	// (Ralph would SIGKILL itself); pid 1 targets init. Never group-signal those.
	if p.Pid <= 1 {
		return p.Kill()
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill() // fall back to the direct child
	}
	return nil
}

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
	// Preserve any other SysProcAttr the caller set; only add Setpgid.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return killProcessTree(cmd.Process) }
	// After Cancel fires, don't wait forever for pipe copies to drain — force the
	// I/O to unblock so Run returns bounded even if a grandchild lingers a moment.
	cmd.WaitDelay = 5 * time.Second
}
