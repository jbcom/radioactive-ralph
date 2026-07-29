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
	// host reaches ~1.1s, so the budget clears that with margin while staying
	// under the ~3s ceiling callers allow for a terminate-and-join, preserving
	// the supervisor's never-block invariant.
	sessionCleanupBudget = 2 * time.Second
)

// cleanupBudget is sessionCleanupBudget, indirected so a test can force the
// deadline branch. Without this seam the re-verification below is UNREACHABLE
// in a unit test: on an idle machine `remaining` empties within one poll, so
// a behavioural test passes with or without the fix and proves nothing.
// Confirmed by mutation -- removing the re-check entirely left the
// behavioural test green.
var cleanupBudget = sessionCleanupBudget

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
	api, err := systemDarwinProcessAPI()
	if err != nil {
		return err
	}
	var remaining []int
	effectiveUID := os.Geteuid()
	if effectiveUID < 0 {
		return fmt.Errorf("agent: invalid effective UID %d", effectiveUID)
	}
	deadline := time.Now().Add(cleanupBudget)
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
			if originalGroupSignaled && member.group == process.Pid {
				continue
			}
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
			if !found || current != member || currentSession(member.pid) != sessionID {
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
			// RE-VERIFY before claiming a leak. `remaining` is built from
			// enumeration ALONE: a member in the already-signalled group is
			// appended above and then `continue`d past every liveness check,
			// so the only way a pid leaves this list is disappearing from a
			// LATER kern.proc.all enumeration.
			//
			// That enumeration lags a completed reap under load. There is a
			// window after the kernel kills a process, before it becomes a
			// zombie, in which it still enumerates as live -- and
			// darwinSessionMembers only filters darwinZombieState, so the
			// window is not covered. At idle the reap lands within one 5ms
			// pass and nothing is observed; under saturation the lag exceeds
			// the whole budget.
			//
			// The result was a FALSE LEAK REPORT: cleanup had worked, the
			// group SIGKILL had landed, and the turn still failed with
			// "still has live members after cleanup". Worse, it wrapped
			// ErrProcessSessionCleanup around an otherwise-correct result,
			// breaking errors.Is sentinel comparisons for callers -- which is
			// how it surfaced, as a prompt-detection test whose error string
			// was correct but had cleanup noise appended.
			//
			// This cannot mask a real leak: a genuinely live in-session member
			// still passes the re-check and is still reported.
			live := remaining[:0]
			for _, pid := range remaining {
				if _, found, err := readDarwinSessionMember(pid); err == nil && found &&
					currentSession(pid) == sessionID {
					live = append(live, pid)
				}
			}
			if len(live) == 0 {
				return nil
			}
			remaining = live
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
