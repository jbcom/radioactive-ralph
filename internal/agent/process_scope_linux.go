//go:build linux

package agent

import (
	"bytes"
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

func reclaimProcessScope(entry string, excludedPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		members, err := linuxProcessScopeMembers(entry, excludedPID)
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return nil
		}
		for _, pid := range members {
			if err := signalLinuxProcessScopeMember(pid, entry); err != nil {
				return err
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent: managed process scope still has live members: %v", members)
		}
		time.Sleep(processScopePollInterval)
	}
}

func linuxProcessScopeMembers(entry string, excludedPID int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("agent: enumerate managed process scope: %w", err)
	}
	want := []byte(entry)
	members := make([]int, 0)
	for _, candidate := range entries {
		if !candidate.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(candidate.Name())
		if err != nil || pid <= 1 || pid == excludedPID {
			continue
		}
		procDir := filepath.Join("/proc", candidate.Name())
		info, err := os.Stat(procDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("agent: inspect managed process scope member: %w", err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) { //nolint:gosec // Linux uid_t is uint32 and Geteuid is non-negative.
			continue
		}
		matched, err := linuxProcessHasScope(pid, want)
		if err != nil {
			return nil, err
		}
		if matched {
			members = append(members, pid)
		}
	}
	return members, nil
}

func linuxProcessHasScope(pid int, want []byte) (bool, error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ")) //nolint:gosec // PID is an enumerated integer.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		// On Linux with YAMA ptrace_scope > 0 (e.g. GitHub Actions runners),
		// /proc/PID/environ is EACCES for processes in a different session
		// even when they share the same UID. Fall back to a stable pidfd
		// handle (confirms same-UID) plus the session-leader signal from
		// /proc/PID/stat (always world-readable): a same-UID session leader
		// that is not the excluded PID is a managed-launch setsid child.
		if errors.Is(err, os.ErrPermission) {
			return linuxProcessHasScopeViaStat(pid, want)
		}
		return false, fmt.Errorf("agent: read managed process scope member %d: %w", pid, err)
	}
	for _, field := range bytes.Split(content, []byte{0}) {
		if bytes.Equal(field, want) {
			return true, nil
		}
	}
	return false, nil
}

// linuxProcessHasScopeViaStat is the EACCES fallback for /proc/PID/environ.
// It confirms same-UID via PidfdOpen (which succeeds regardless of
// ptrace_scope), then reads /proc/PID/stat (always world-readable) to get
// the session ID. A same-UID session leader (SID == PID) that is not PID 1
// is a managed-launch setsid descendant.
func linuxProcessHasScopeViaStat(pid int, want []byte) (bool, error) {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, nil // not same-UID or unreachable; skip
	}
	defer func() { _ = unix.Close(fd) }()

	statContent, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")) //nolint:gosec // PID is an enumerated integer.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("agent: stat managed process scope member %d: %w", pid, err)
	}
	endCommand := strings.LastIndex(string(statContent), ") ")
	if endCommand < 0 {
		return false, nil
	}
	fields := strings.Fields(string(statContent[endCommand+2:]))
	if len(fields) < 5 {
		return false, nil
	}
	sid, err := strconv.Atoi(fields[4])
	if err != nil {
		return false, nil
	}
	// A session leader (SID == PID) created by Setsid in a managed launch.
	return sid == pid && pid > 1, nil
}

func signalLinuxProcessScopeMember(pid int, entry string) error {
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("agent: open stable handle for managed process %d: %w", pid, err)
	}
	defer func() { _ = unix.Close(fd) }()

	matched, err := linuxProcessHasScope(pid, []byte(entry))
	if err != nil {
		return err
	}
	if !matched {
		return nil
	}
	if err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("agent: kill managed process %d: %w", pid, err)
	}
	return nil
}
