//go:build !windows

package supervisor

import (
	"os"
	"syscall"
)

// pidAlive reports whether pid identifies a live process on this host.
// os.FindProcess always succeeds on POSIX (it does not check existence),
// so liveness requires sending signal 0, which the kernel validates
// without actually delivering anything.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
