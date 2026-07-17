//go:build windows

package provider

import (
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
