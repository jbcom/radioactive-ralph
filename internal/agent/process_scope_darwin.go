//go:build darwin

package agent

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func reclaimProcessScope(entry string, excludedPID int, timeout time.Duration) error {
	api, err := systemDarwinProcessAPI()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		members, err := darwinProcessScopeMembers(entry, excludedPID)
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return nil
		}
		for _, pid := range members {
			token, err := api.auditTokenForPID(pid)
			if errors.Is(err, errDarwinProcessGone) {
				continue
			}
			if err != nil {
				return err
			}
			matched, err := darwinProcessHasScope(pid, []byte(entry))
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
			if err := api.signalAuditToken(token, syscall.SIGKILL); err != nil {
				return fmt.Errorf("agent: audit-token kill managed process %d: %w", pid, err)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent: managed process scope still has live members: %v", members)
		}
		time.Sleep(processScopePollInterval)
	}
}

func darwinProcessScopeMembers(entry string, excludedPID int) ([]int, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, fmt.Errorf("agent: enumerate managed process scope: %w", err)
	}
	want := []byte(entry)
	effectiveUID := uint32(os.Geteuid()) //nolint:gosec // Darwin uid_t is uint32 and Geteuid is non-negative.
	members := make([]int, 0)
	for i := range processes {
		candidate := &processes[i]
		pid := int(candidate.Proc.P_pid)
		if pid <= 1 || pid == excludedPID || candidate.Proc.P_stat == darwinZombieState ||
			candidate.Eproc.Ucred.Uid != effectiveUID {
			continue
		}
		matched, err := darwinProcessHasScope(pid, want)
		if err != nil {
			return nil, err
		}
		if matched {
			members = append(members, pid)
		}
	}
	return members, nil
}

func darwinProcessHasScope(pid int, want []byte) (bool, error) {
	content, err := unix.SysctlRaw("kern.procargs2", pid)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.EIO) || errors.Is(err, syscall.EINVAL) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("agent: read managed process scope member %d: %w", pid, err)
	}
	for _, field := range bytes.Split(content, []byte{0}) {
		if bytes.Equal(field, want) {
			return true, nil
		}
	}
	return false, nil
}
