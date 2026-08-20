//go:build !windows

package wsldistro

import "context"

// Check always reports "not applicable" on non-Windows platforms: there is
// nothing to check or provision there.
func Check(_ context.Context) Status {
	return Status{Applicable: false, Detail: "not applicable (native Unix pty support)"}
}
