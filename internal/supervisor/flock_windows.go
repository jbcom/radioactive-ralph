//go:build windows

package supervisor

import (
	"errors"
	"os"
)

// acquirePIDLock is not supported on native Windows — the supervisor
// runs on POSIX only (WSL2 or Linux). Provided here for compile
// completeness; any call fails with ErrNotSupported.
func acquirePIDLock(_ string) (*os.File, error) {
	return nil, errors.New("supervisor: PID flock not supported on Windows; use WSL2")
}
