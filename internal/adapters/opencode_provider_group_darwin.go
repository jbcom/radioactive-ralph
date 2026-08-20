//go:build darwin

package adapters

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const openCodeDarwinZombieState = 5

func waitOpenCodeProviderExit(process *os.Process) error {
	if process == nil || process.Pid <= 1 {
		return fmt.Errorf("invalid provider process")
	}
	// x/sys does not expose waitid on Darwin. The kernel ABI takes the POSIX
	// id type, child PID, siginfo buffer, and options. WNOWAIT deliberately
	// leaves the exited group leader waitable until Ralph reclaims the group.
	var info [128]byte
	for {
		//nolint:gosec,staticcheck // Darwin x/sys exposes neither the libc wrapper nor siginfo_t; the fixed local buffer is kernel output only.
		_, _, errno := unix.Syscall6(
			unix.SYS_WAITID,
			1, // P_PID from Darwin sys/wait.h
			uintptr(process.Pid),
			uintptr(unsafe.Pointer(&info[0])),
			unix.WEXITED|unix.WNOWAIT,
			0,
			0,
		)
		if errors.Is(errno, unix.EINTR) {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func openCodeProviderGroupLive(group int) (bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	if err != nil {
		return false, fmt.Errorf("enumerate provider process group: %w", err)
	}
	for i := range processes {
		process := &processes[i]
		if int(process.Eproc.Pgid) == group && int(process.Proc.P_pid) > 1 &&
			process.Proc.P_stat != openCodeDarwinZombieState {
			return true, nil
		}
	}
	return false, nil
}
