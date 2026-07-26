//go:build !windows

package agent

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestEarlyStartCleanupDirectKillFailureNeverWaitsOnLiveChild(t *testing.T) {
	injectedErr := errors.New("injected early stable-handle kill failure")
	for iteration := range 10 {
		cmd := exec.Command("sh", "-c", "sleep 300")
		if err := cmd.Start(); err != nil {
			t.Fatalf("iteration %d Start fixture: %v", iteration, err)
		}
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("iteration %d Pipe: %v", iteration, err)
		}
		_ = readEnd.Close()

		started := time.Now()
		cleanupErr := abortStartedProcess(
			cmd,
			writeEnd,
			errors.New("injected setup failure"),
			func(*os.Process) terminationOutcome {
				return terminationOutcome{
					terminationErr: errors.Join(ErrProcessTermination, injectedErr),
				}
			},
		)
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("iteration %d cleanup took %v; cmd.Wait ran on live child",
				iteration, elapsed)
		}
		if !errors.Is(cleanupErr, ErrProcessTermination) ||
			!errors.Is(cleanupErr, injectedErr) {
			t.Fatalf("iteration %d cleanup = %v, want termination failure",
				iteration, cleanupErr)
		}
		if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
			t.Fatalf("iteration %d cleanup falsely completed live child: %v",
				iteration, err)
		}

		if err := cmd.Process.Kill(); err != nil &&
			!errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("iteration %d fixture kill: %v", iteration, err)
		}
		_ = cmd.Wait()
	}
}
