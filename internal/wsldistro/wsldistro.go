// Package wsldistro manages the dedicated, hidden "radioactive-ralph" WSL2
// distro that Windows provider dispatch runs inside (see
// internal/agent/pty_start_windows.go and
// docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md). It
// wraps github.com/ubuntu/gowsl for lifecycle management ONLY -- register,
// check state, unregister -- never for command execution: gowsl's own
// Distro.Command()/WslLaunch API was tested directly and reproduces the
// exact same broken-stdin-EOF symptom ConPTY has. Actual dispatch goes
// through wsl.exe as a plain os/exec subprocess instead (pty_start_windows.go).
//
// Real logic lives in wsldistro_windows.go; wsldistro_other.go is a
// same-signature stub for every other platform so callers (internal/doctor)
// don't need their own build tags.
package wsldistro

// Status reports the state of the managed WSL2 distro.
type Status struct {
	// Applicable is false on any non-Windows platform: there is nothing to
	// check or provision there.
	Applicable bool
	// WSLAvailable reports whether wsl.exe was found on PATH at all.
	WSLAvailable bool
	// DistroRegistered reports whether the distro is already registered.
	// Meaningless (always false) when WSLAvailable is false.
	DistroRegistered bool
	// Detail is a short human-readable summary for doctor/status output.
	Detail string
}
