//go:build windows

package agent

// disablePTYEcho is a no-op on native Windows: dispatch goes through
// wsl.exe as a plain subprocess (see pty_start_windows.go), not a pty, so
// there is no pty line discipline to configure.
func disablePTYEcho(_ ptyMaster) error { return nil }
