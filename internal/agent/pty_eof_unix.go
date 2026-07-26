//go:build !windows

package agent

import (
	"errors"
	"syscall"
)

// Unix pty masters commonly report EIO when the slave side closes after a
// normal child exit. Treat that platform convention as EOF, not a stream
// failure.
func isPTYEOF(err error) bool {
	return errors.Is(err, syscall.EIO)
}
