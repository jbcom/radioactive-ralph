//go:build !windows

package wsldistro

import "context"

// Check always reports "not applicable" on non-Windows platforms: there is
// nothing to check or provision there.
func Check(_ context.Context) Status {
	return Status{Applicable: false, Detail: "not applicable (native Unix pty support)"}
}

// EnsureRegistered is a no-op on non-Windows platforms: there is no distro
// to provision there. Same-signature stub so callers (pty_start_*.go) don't
// need their own build tags around calling it.
func EnsureRegistered(_ context.Context) error { return nil }
