//go:build windows

package agent

import (
	"os/exec"
	"time"
)

// setCancelKillsGroup on Windows keeps exec.CommandContext's default cancel
// (no POSIX group to signal) but bounds the post-cancel wait via WaitDelay. The
// pty agent path is unsupported on native Windows anyway (Start returns
// ErrPTYUnsupported before any child is spawned). Must be called before the
// process starts. A native ConPTY agent path, if added later, would reap
// children via a Job Object or CREATE_NEW_PROCESS_GROUP + taskkill /T here.
func setCancelKillsGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}
