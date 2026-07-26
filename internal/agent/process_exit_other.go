//go:build aix || dragonfly || freebsd || netbsd || openbsd || solaris

package agent

import (
	"os"
)

const processExitObservationSupported = false

func waitProcessExited(*os.Process, <-chan struct{}) error {
	return ErrProcessExitObservationUnsupported
}

func processAlreadyExited(*os.Process) (bool, error) {
	return false, ErrProcessExitObservationUnsupported
}
