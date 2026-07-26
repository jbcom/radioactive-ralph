//go:build linux

package agent

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const processExitObservationSupported = true

// waitProcessExited observes exit without reaping. Holding the child as a
// zombie until processLifecycle claims it keeps its PID/PGID unavailable for
// reuse, so the final process-group cleanup cannot target an unrelated group.
func waitProcessExited(
	probe func() (bool, error),
	stop <-chan struct{},
) error {
	// The lifecycle-guarded probe uses pidfd when available and waitid WNOWAIT
	// otherwise. Keeping acquisition inside that guard is what prevents a raw
	// PID lookup from racing cmd.Wait and PID reuse.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		exited, err := probe()
		if err != nil {
			return err
		}
		if exited {
			return nil
		}
		select {
		case <-stop:
			return errProcessExitObservationStopped
		case <-ticker.C:
		}
	}
}

func processAlreadyExited(process *os.Process) (bool, error) {
	fd, err := unix.PidfdOpen(process.Pid, 0)
	if err == nil {
		defer func() { _ = unix.Close(fd) }()
		pollFD := int32(fd) //nolint:gosec // kernel file descriptors fit pollfd.fd
		poll := []unix.PollFd{{Fd: pollFD, Events: unix.POLLIN}}
		n, err := unix.Poll(poll, 0)
		if err != nil {
			return false, err
		}
		if n > 0 && poll[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return false, fmt.Errorf(
				"agent: pidfd probe failed with revents %#x",
				poll[0].Revents,
			)
		}
		return n > 0 && poll[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0, nil
	}
	// Match waitProcessExited's older-kernel path. WNOHANG makes this safe
	// inside lifecycle.force's mutex; WNOWAIT preserves the zombie so the
	// caller can perform group cleanup before cmd.Wait reaps it.
	var info unix.Siginfo
	err = unix.Waitid(
		unix.P_PID,
		process.Pid,
		&info,
		unix.WEXITED|unix.WNOWAIT|unix.WNOHANG,
		nil,
	)
	if err != nil {
		return false, err
	}
	return info.Signo != 0, nil
}
