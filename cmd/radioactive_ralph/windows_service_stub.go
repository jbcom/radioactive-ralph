//go:build !windows

package main

// maybeRunWindowsService is an early process-entry hook on Windows. Other
// platforms always continue through the ordinary Cobra/signal-context path.
func maybeRunWindowsService() (handled bool, exitCode int) {
	return false, 0
}
