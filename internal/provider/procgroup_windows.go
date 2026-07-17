//go:build windows

package provider

import (
	"os"
	"os/exec"
	"time"
)

// setProcessGroupKill on Windows keeps exec.CommandContext's default cancel
// (there's no POSIX process group to signal). WaitDelay still bounds the wait
// for pipe copies after cancellation so a lingering child can't block Run
// forever. A future ConPTY/Job-Object path could reap child trees here.
func setProcessGroupKill(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

// killProcessTree on Windows kills the process (no POSIX group to signal).
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
