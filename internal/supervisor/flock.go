//go:build !windows

package supervisor

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// acquirePIDLock opens path with O_CREATE|O_WRONLY, takes an
// exclusive non-blocking flock, and writes the current PID.
//
// If another process already holds the lock, returns an error.
// The returned *os.File must be retained by the caller; closing it
// releases the lock.
func acquirePIDLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is workspace-owned
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}
	// f.Fd() returns uintptr; syscall.Flock wants int. On every
	// platform we support (darwin/linux, 64-bit), both are 64-bit.
	// The conversion cannot actually lose bits in practice — file
	// descriptors are small integers.
	fd := int(f.Fd()) //nolint:gosec // fd values always fit in int
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock %s: %w (another supervisor may be running)", path, err)
	}
	// Truncate and write our PID.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("truncate pid file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seek pid file: %w", err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write pid: %w", err)
	}
	return f, nil
}
