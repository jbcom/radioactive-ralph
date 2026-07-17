//go:build windows

package agent

import (
	"os"
	"os/exec"
	"time"
)

// setCancelKillsGroup on Windows keeps exec.CommandContext's default cancel
// (no POSIX group to signal) but bounds the post-cancel wait via WaitDelay. The
// pty agent path is unsupported on native Windows anyway. Must be called before
// the process starts.
func setCancelKillsGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

// killProcessTree kills the process. On Windows the pty-backed agent path is
// unsupported (Start returns ErrPTYUnsupported before any child is spawned), so
// there is no session/process-group tree to reap here — the direct kill is the
// correct and only behavior. (A native ConPTY agent path, if added later, would
// need a Job Object or CREATE_NEW_PROCESS_GROUP + taskkill /T to reap children.)
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
