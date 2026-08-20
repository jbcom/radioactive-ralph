//go:build linux

package agent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
		// even when they share the same UID. We cannot safely determine
		// whether this process belongs to the managed launch scope without
		// reading its environment, so fail closed rather than risking killing
		// an unrelated same-UID session leader.
		if errors.Is(err, os.ErrPermission) {
			return false, fmt.Errorf("agent: cannot verify managed process scope member %d (environ unreadable): %w", pid, err)
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
