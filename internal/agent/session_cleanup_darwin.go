//go:build darwin

package agent

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const (
	sessionCleanupAttempts = 100
	sessionCleanupInterval = 5 * time.Millisecond
)

// cleanupOriginalProcessSession proves ordinary process-group cleanup and
// detects setpgrp(2) escape on Darwin. Darwin exposes no pidfd-equivalent
// stable descendant handle; signalling a discovered raw PID/PGID could hit a
// recycled unrelated process. Preserve that safety invariant by reporting an
// escaped live member instead. The terminal-aware PTY reader still guarantees
// the descendant cannot wedge Agent.Wait.
func cleanupOriginalProcessSession(
	process *os.Process,
	originalGroupSignaled bool,
) error {
	if process == nil || process.Pid <= 1 {
		return nil
	}
	// creack/pty Start applies Setsid before exec, making the direct child's
	// PID the original session ID. Darwin getsid(2) returns ESRCH for a zombie,
	// so derive the invariant from launch rather than probing the exited leader.
	sessionID := process.Pid
	var ordinaryGroupMembers []int
	for range sessionCleanupAttempts {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
		if err != nil {
			return fmt.Errorf("agent: enumerate PTY session: %w", err)
		}
		ordinaryGroupMembers = ordinaryGroupMembers[:0]
		for i := range processes {
			candidate := &processes[i]
			pid := int(candidate.Proc.P_pid)
			if pid <= 1 || pid == process.Pid ||
				candidate.Proc.P_stat == darwinZombieState {
				continue
			}
			candidateSession, sessionErr := unix.Getsid(pid)
			if sessionErr != nil || candidateSession != sessionID {
				continue
			}
			if int(candidate.Eproc.Pgid) != process.Pid || !originalGroupSignaled {
				return fmt.Errorf(
					"agent: Darwin cannot safely signal PTY session member %d in process group %d",
					pid,
					candidate.Eproc.Pgid,
				)
			}
			ordinaryGroupMembers = append(ordinaryGroupMembers, pid)
		}
		if len(ordinaryGroupMembers) == 0 {
			return nil
		}
		time.Sleep(sessionCleanupInterval)
	}
	return fmt.Errorf(
		"agent: PTY process group %d still has live members after cleanup: %v",
		process.Pid,
		ordinaryGroupMembers,
	)
}
