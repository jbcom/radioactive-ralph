//go:build darwin

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

const darwinZombieState = 5

// A fresh EVFILT_PROC registration is not guaranteed to replay NOTE_EXIT for a
// child that was already a zombie before registration. kern.proc.pid exposes
// that exact unreaped state without consuming it.
func processAlreadyExited(process *os.Process) (bool, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", process.Pid)
	if err != nil {
		return false, err
	}
	return int(info.Proc.P_pid) == process.Pid &&
		info.Proc.P_stat == darwinZombieState, nil
}
