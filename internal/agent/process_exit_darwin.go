//go:build darwin

package agent

import (
	"time"
)

const processExitObservationSupported = true

// Darwin's EVFILT_PROC registration is not guaranteed to replay NOTE_EXIT when
// the child becomes a zombie in the registration gap. Polling kern.proc.pid's
// SZOMB state is non-reaping, cannot miss that stable state, and is cancellable
// when an unrecoverable process-control error terminates the Agent.
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
