//go:build windows

package agent

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const processExitObservationSupported = true

func waitProcessExited(
	probe func() (bool, error),
	stop <-chan struct{},
) error {
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
	exited := false
	var waitErr error
	err := process.WithHandle(func(handle uintptr) {
		status, err := windows.WaitForSingleObject(windows.Handle(handle), 0)
		if err != nil {
			waitErr = err
			return
		}
		exited = status == windows.WAIT_OBJECT_0
	})
	if err != nil {
		return false, err
	}
	return exited, waitErr
}
