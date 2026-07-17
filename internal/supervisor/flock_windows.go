//go:build windows

package supervisor

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

// acquirePIDLock is the Windows counterpart to the flock-based POSIX
// implementation in flock.go. See that file's doc comment for the role
// this lock plays alongside the IPC socket bind.
func acquirePIDLock(path string) (*os.File, error) {
	// Share READ|WRITE|DELETE: mutual exclusion comes from LockFileEx below,
	// NOT from a zero share-mode. Sharing DELETE in particular lets another
	// process (or t.TempDir cleanup on CI) remove/rename the pid file while
	// this handle is still open, instead of failing with "the process cannot
	// access the file because it is being used by another process"; sharing
	// READ lets shouldReclaim's readPIDFile open it to inspect a possibly
	// stale pid.
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open pid file: %w", err)
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("lock %s: %w (another supervisor may already be running)", path, err)
	}
	f := os.NewFile(uintptr(handle), path)
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
