//go:build linux

package adapters

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func waitOpenCodeProviderExit(process *os.Process) error {
	if process == nil || process.Pid <= 1 {
		return fmt.Errorf("invalid provider process")
	}
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}

func openCodeProviderGroupLive(group int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, fmt.Errorf("enumerate provider process group: %w", err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || !entry.IsDir() {
			continue
		}
		memberGroup, state, found, err := readOpenCodeLinuxProcess(pid)
		if err != nil {
			return false, err
		}
		if found && memberGroup == group && state != 'Z' {
			return true, nil
		}
	}
	return false, nil
}

func readOpenCodeLinuxProcess(pid int) (group int, state byte, found bool, err error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")) //nolint:gosec // pid is parsed from a numeric /proc entry
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("inspect provider process %d: %w", pid, err)
	}
	endCommand := strings.LastIndex(string(content), ") ")
	if endCommand < 0 {
		return 0, 0, false, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(content[endCommand+2:]))
	if len(fields) < 3 || len(fields[0]) != 1 {
		return 0, 0, false, fmt.Errorf("incomplete /proc/%d/stat", pid)
	}
	group, err = strconv.Atoi(fields[2])
	if err != nil {
		return 0, 0, false, fmt.Errorf("parse /proc/%d process group: %w", pid, err)
	}
	return group, fields[0][0], true, nil
}
