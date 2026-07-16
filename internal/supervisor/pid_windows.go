//go:build windows

package supervisor

import "os"

// pidAlive reports whether pid identifies a live process on this host.
// On Windows, os.FindProcess opens a real handle via OpenProcess, which
// fails for a PID that no longer exists, so a successful open is
// sufficient evidence of liveness (unlike POSIX, no signal probe is
// needed).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
