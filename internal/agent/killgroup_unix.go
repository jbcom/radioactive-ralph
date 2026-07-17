//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// setCancelKillsGroup makes ctx cancellation (from KillWorker, supervisor
// shutdown, or the orchestrator's stall timeout) reap the whole process GROUP.
// exec.CommandContext's DEFAULT cancel is cmd.Process.Kill() — the direct child
// only — so without this a ctx-cancelled turn would leave the agent's
// grandchildren orphaned exactly as an explicit Kill() would have before the
// process-group fix. Overriding cmd.Cancel routes the automatic cancel through
// the same group kill. WaitDelay bounds the post-kill wait for the pty copy to
// drain. Must be called BEFORE pty.Start (which starts the process).
func setCancelKillsGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return killProcessTree(cmd.Process)
	}
	cmd.WaitDelay = 5 * time.Second
}

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
	// CRITICAL guard: syscall.Kill(-pid, ...) with pid 0 signals the CALLER's own
	// process group (Ralph would SIGKILL itself); pid 1 targets init's group.
	// Never group-signal those — fall back to the direct kill, which for pid 1 is
	// itself a no-op-or-EPERM. A real agent child always has pid > 1.
	if p.Pid <= 1 {
		return p.Kill()
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone, or (defensively) the child wasn't a
		// group leader; fall back to the direct-process kill.
		return p.Kill()
	}
	return nil
}
