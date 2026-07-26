//go:build linux

package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	sessionCleanupAttempts = 100
	sessionCleanupInterval = 5 * time.Millisecond
)

// cleanupOriginalProcessSession reclaims descendants that moved to another
// process group with setpgrp(2) but remain in the PTY's original session.
// Linux pidfds make each descendant signal stable against PID reuse. A child
// that creates a new session with setsid(2) is intentionally undiscoverable by
// this session-scoped contract.
func cleanupOriginalProcessSession(
	process *os.Process,
	originalGroupSignaled bool,
) error {
	if process == nil || process.Pid <= 1 {
		return nil
	}
	sessionID := process.Pid // creack/pty Start makes the child the session leader.
	var remaining []int
	for range sessionCleanupAttempts {
		members, err := linuxSessionMembers(sessionID)
		if err != nil {
			return err
		}
		remaining = remaining[:0]
		for _, member := range members {
			if member.pid == sessionID || member.state == 'Z' {
				continue
			}
			if !originalGroupSignaled || member.group != sessionID {
				if err := signalLinuxSessionMember(member.pid, sessionID); err != nil {
					return err
				}
			}
			remaining = append(remaining, member.pid)
		}
		if len(remaining) == 0 {
			return nil
		}
		time.Sleep(sessionCleanupInterval)
	}
	return fmt.Errorf(
		"agent: original PTY session %d still has live members after cleanup: %v",
		sessionID,
		remaining,
	)
}

type linuxSessionMember struct {
	pid     int
	session int
	group   int
	state   byte
}

func linuxSessionMembers(sessionID int) ([]linuxSessionMember, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("agent: enumerate /proc for PTY session: %w", err)
	}
	members := make([]linuxSessionMember, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		member, found, err := readLinuxSessionMember(pid)
		if err != nil {
			return nil, err
		}
		if found && member.session == sessionID {
			members = append(members, member)
		}
	}
	return members, nil
}

func readLinuxSessionMember(pid int) (linuxSessionMember, bool, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	// pid is parsed from a numeric /proc directory name; it cannot inject an
	// arbitrary path.
	content, err := os.ReadFile(path) //nolint:gosec
	if errors.Is(err, os.ErrNotExist) {
		return linuxSessionMember{}, false, nil
	}
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return linuxSessionMember{}, false, nil
		}
		return linuxSessionMember{}, false, fmt.Errorf(
			"agent: read process %d session: %w",
			pid,
			err,
		)
	}
	// comm is parenthesized and may contain spaces or right parentheses. The
	// final ") " is the only stable delimiter before state, ppid, pgrp, sid.
	endCommand := strings.LastIndex(string(content), ") ")
	if endCommand < 0 {
		return linuxSessionMember{}, false, fmt.Errorf(
			"agent: malformed /proc/%d/stat",
			pid,
		)
	}
	fields := strings.Fields(string(content[endCommand+2:]))
	if len(fields) < 4 || len(fields[0]) != 1 {
		return linuxSessionMember{}, false, fmt.Errorf(
			"agent: incomplete /proc/%d/stat",
			pid,
		)
	}
	session, err := strconv.Atoi(fields[3])
	if err != nil {
		return linuxSessionMember{}, false, fmt.Errorf(
			"agent: parse process %d session: %w",
			pid,
			err,
		)
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return linuxSessionMember{}, false, fmt.Errorf(
			"agent: parse process %d group: %w",
			pid,
			err,
		)
	}
	return linuxSessionMember{
		pid:     pid,
		session: session,
		group:   group,
		state:   fields[0][0],
	}, true, nil
}

func signalLinuxSessionMember(pid, sessionID int) error {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("agent: open stable handle for session member %d: %w", pid, err)
	}
	defer func() { _ = unix.Close(fd) }()

	// Revalidate after acquiring the stable handle. If the numeric PID was
	// recycled between enumeration and PidfdOpen, never signal the replacement.
	member, found, err := readLinuxSessionMember(pid)
	if err != nil {
		return err
	}
	if !found || member.session != sessionID || member.state == 'Z' {
		return nil
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil &&
		!errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("agent: kill PTY session member %d: %w", pid, err)
	}
	return nil
}
