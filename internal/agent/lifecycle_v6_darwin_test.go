//go:build darwin

package agent

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func verifyNoLiveOriginalSessionDescendants(process *os.Process) error {
	if process == nil || process.Pid <= 1 {
		return nil
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return fmt.Errorf("enumerate PTY session: %w", err)
	}
	live := make([]int, 0)
	for i := range processes {
		candidate := &processes[i]
		pid := int(candidate.Proc.P_pid)
		if pid <= 1 || pid == process.Pid ||
			candidate.Proc.P_stat == darwinZombieState {
			continue
		}
		candidateSession, sessionErr := unix.Getsid(pid)
		if sessionErr == nil && candidateSession == process.Pid {
			live = append(live, pid)
		}
	}
	if len(live) == 0 {
		return nil
	}
	return fmt.Errorf(
		"original PTY session %d still has live descendants: %v",
		process.Pid,
		live,
	)
}
