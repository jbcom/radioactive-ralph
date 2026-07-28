//go:build darwin

package agent

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	sessionCleanupInterval = 5 * time.Millisecond
	// sessionCleanupBudget bounds reclamation in WALL-CLOCK time, not in poll
	// attempts. An attempt count multiplied by a sleep silently shrinks under
	// CPU contention: each pass also pays for a kern.proc.all enumeration plus
	// a getsid(2) per process, so on an oversubscribed host 100 attempts elapse
	// long before 100*interval of real time and abort a converging cleanup.
	// Measured convergence for a 32-descendant tree on a loaded 16-core macOS
	// host reaches ~1.1s, so the budget clears that with margin while still
	// bounding the supervisor's never-block invariant.
	sessionCleanupBudget = 5 * time.Second
)

type darwinSessionMember struct {
	pid       int
	parentPID int
	group     int
	euid      uint32
	startSec  int64
	startUsec int32
}

// cleanupOriginalProcessSession reclaims descendants that moved process group
// while remaining in Ralph's PTY session. Darwin's
// proc_signal_with_audittoken is the pidfd-equivalent safety primitive here:
// the kernel validates the token's PID version, so a recycled numeric PID is
// never signaled. Enumeration is revalidated after token acquisition and
// escaped members are killed leaf-first.
func cleanupOriginalProcessSession(process *os.Process) error {
	if process == nil || process.Pid <= 1 {
		return nil
	}
	// creack/pty Start applies Setsid before exec, making the direct child's
	// PID the original session ID. Darwin getsid(2) returns ESRCH for a zombie,
	// so derive the invariant from launch rather than probing the exited leader.
	sessionID := process.Pid
	api, err := systemDarwinProcessAPI()
	if err != nil {
		return err
	}
	var remaining []int
	effectiveUID := os.Geteuid()
	if effectiveUID < 0 {
		return fmt.Errorf("agent: invalid effective UID %d", effectiveUID)
	}
	deadline := time.Now().Add(sessionCleanupBudget)
	for {
		members, err := darwinSessionMembers(sessionID)
		if err != nil {
			return err
		}
		sortDarwinMembersLeafFirst(members)
		remaining = remaining[:0]
		for _, member := range members {
			if member.pid == process.Pid {
				continue
			}
			remaining = append(remaining, member.pid)
			// Re-signal every member on every pass. The caller's single
			// kill(-pgid) cannot reach a descendant forked after that signal
			// landed, so membership in the leader's group is not evidence the
			// member was ever signalled.
			// Darwin uid_t is uint32; effectiveUID was checked non-negative.
			if member.euid != uint32(effectiveUID) { //nolint:gosec // Darwin uid_t ABI
				return fmt.Errorf("agent: refuse to signal PTY session member %d owned by another user", member.pid)
			}
			token, err := api.auditTokenForPID(member.pid)
			// A member can exit between enumeration and this lookup. Darwin
			// signals that as a Mach failure, never as ESRCH, so match the
			// sentinel: a vanished member is already reclaimed, not a failure.
			if errors.Is(err, errDarwinProcessGone) {
				continue
			}
			if err != nil {
				return err
			}
			if token.euid() != member.euid {
				return fmt.Errorf("agent: audit-token UID changed for PTY session member %d", member.pid)
			}
			current, found, err := readDarwinSessionMember(member.pid)
			if err != nil {
				return err
			}
			// Identity is (pid, start time, euid): none can change for a live
			// process, so together they prove the PID was not recycled. Group and
			// parent are deliberately excluded — a descendant may setpgrp(2) or
			// reparent to launchd between enumeration and now, and neither may
			// exempt it from the kill.
			if !found ||
				current.pid != member.pid ||
				current.startSec != member.startSec ||
				current.startUsec != member.startUsec ||
				current.euid != member.euid ||
				currentSession(member.pid) != sessionID {
				continue
			}
			if err := api.signalAuditToken(token, syscall.SIGKILL); err != nil {
				return fmt.Errorf("agent: audit-token kill PTY session member %d: %w", member.pid, err)
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(sessionCleanupInterval)
	}
	return fmt.Errorf(
		"agent: PTY process group %d still has live members after cleanup: %v",
		process.Pid,
		remaining,
	)
}

func darwinSessionMembers(sessionID int) ([]darwinSessionMember, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("agent: enumerate PTY session: %w", err)
	}
	members := make([]darwinSessionMember, 0)
	for i := range processes {
		candidate := &processes[i]
		pid := int(candidate.Proc.P_pid)
		if pid <= 1 || candidate.Proc.P_stat == darwinZombieState || currentSession(pid) != sessionID {
			continue
		}
		members = append(members, darwinMemberFromKinfo(candidate))
	}
	return members, nil
}

func readDarwinSessionMember(pid int) (darwinSessionMember, bool, error) {
	candidate, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT) {
		return darwinSessionMember{}, false, nil
	}
	if err != nil {
		return darwinSessionMember{}, false, fmt.Errorf("agent: inspect Darwin process %d: %w", pid, err)
	}
	if candidate.Proc.P_stat == darwinZombieState {
		return darwinSessionMember{}, false, nil
	}
	return darwinMemberFromKinfo(candidate), true, nil
}

func darwinMemberFromKinfo(candidate *unix.KinfoProc) darwinSessionMember {
	return darwinSessionMember{
		pid:       int(candidate.Proc.P_pid),
		parentPID: int(candidate.Eproc.Ppid),
		group:     int(candidate.Eproc.Pgid),
		euid:      candidate.Eproc.Ucred.Uid,
		startSec:  candidate.Proc.P_starttime.Sec,
		startUsec: candidate.Proc.P_starttime.Usec,
	}
}

func currentSession(pid int) int {
	session, err := unix.Getsid(pid)
	if err != nil {
		return -1
	}
	return session
}

func sortDarwinMembersLeafFirst(members []darwinSessionMember) {
	byPID := make(map[int]darwinSessionMember, len(members))
	for _, member := range members {
		byPID[member.pid] = member
	}
	depth := func(member darwinSessionMember) int {
		seen := map[int]bool{member.pid: true}
		current := member
		value := 0
		for {
			parent, ok := byPID[current.parentPID]
			if !ok || seen[parent.pid] {
				return value
			}
			seen[parent.pid] = true
			value++
			current = parent
		}
	}
	slices.SortFunc(members, func(a, b darwinSessionMember) int {
		aDepth, bDepth := depth(a), depth(b)
		if aDepth != bDepth {
			return bDepth - aDepth
		}
		return b.pid - a.pid
	})
}
