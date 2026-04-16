//go:build !windows

// Package runtime owns the durable repo service engine and its local process
// coordination helpers.
package runtime

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"syscall"
)

func acquirePIDLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // service-owned path
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}
	fd := f.Fd()
	if fd > math.MaxInt {
		_ = f.Close()
		return nil, fmt.Errorf("fd %d exceeds max int", fd)
	}
	if err := syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock %s: %w (another repo service may already be running)", path, err)
	}
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
