//go:build !darwin && !linux && !windows

package orch

import (
	"errors"
	"os"
)

var errContainedOpenUnsupported = errors.New(
	"orch: no-follow open unsupported on this platform")

// openContainedFile FAILS CLOSED on a platform without a safe no-follow open.
//
// Degrading to os.Open here would silently turn the containment guarantee into
// nothing on exactly the platforms nobody tests, which is worse than refusing:
// a caller would pin a hash of whatever a symlink pointed at and believe it had
// pinned the declared file. Mirrors
// internal/provider/result_open_unsupported.go.
func openContainedFile(string) (*os.File, error) {
	return nil, errContainedOpenUnsupported
}
