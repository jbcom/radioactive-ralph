//go:build !darwin && !linux && !windows

package agent

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func startPTY(cmd *exec.Cmd, _ bool) (*os.File, error) {
	return pty.Start(cmd)
}
