//go:build !windows

package agent

import (
	"os"
	"syscall"
)

// killProcessTree SIGKILLs the process AND its whole process group, so any
// grandchildren the agent spawned (a shell tool, a git subprocess, an MCP
// server) die with it rather than orphaning against the checkout and continuing
// to consume CPU/tokens/network after Ralph believes the turn is reclaimed —
// the never-block control invariant demands the WHOLE agent tree be reaped.
//
// creack/pty starts the child with Setsid (a new session), so the child is a
// session/process-group leader and its PGID == its PID. Signalling the negative
// PID (syscall.Kill(-pid, SIGKILL)) therefore delivers SIGKILL to every process
// in that group. Falls back to killing just the process if the group signal
// fails (e.g. the leader already reaped the group).
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone, or (defensively) the child wasn't a
		// group leader; fall back to the direct-process kill.
		return p.Kill()
	}
	return nil
}
