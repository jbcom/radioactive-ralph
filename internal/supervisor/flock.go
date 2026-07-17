//go:build !windows

package supervisor

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"syscall"
)

// acquirePIDLock takes an exclusive, non-blocking flock on path, truncates
// it, and writes the current PID. It is the "is a live supervisor already
// running" mutex of last resort — the primary single-instance mechanism is
// binding the IPC socket (see Acquire in discovery.go); this lockfile lets
// Acquire distinguish a stale socket left by a crashed supervisor (dead PID)
// from one that is still genuinely alive but briefly unresponsive.
func acquirePIDLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // supervisor-owned path
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
		return nil, fmt.Errorf("flock %s: %w (another supervisor may already be running)", path, err)
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
