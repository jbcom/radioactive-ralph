//go:build darwin

package adapters

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const openCodeDarwinZombieState = 5

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
