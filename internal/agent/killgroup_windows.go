//go:build windows

package agent

import (
	"errors"
	"os"
)

// Native Windows pty execution is unsupported. The lifecycle still uses the
// stable process handle so its platform tests prove natural/forced ordering. A
// future ConPTY implementation must replace this with an owned Job Object.
func cleanupExitedProcessTree(*os.Process) error { return nil }

func terminateProcessTree(process *os.Process) terminationOutcome {
	if process == nil {
		return terminationOutcome{}
	}
	err := process.Kill()
	if err == nil {
		return terminationOutcome{}
	}
	if errors.Is(err, os.ErrProcessDone) {
		return terminationOutcome{alreadyExited: true}
	}
	return terminationOutcome{terminationErr: errors.Join(ErrProcessTermination, err)}
}
