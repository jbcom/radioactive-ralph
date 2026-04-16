//go:build windows

package runtime

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

func acquirePIDLock(path string) (*os.File, error) {
	handle, err := windows.CreateFile(
		windows.StringToUTF16Ptr(path),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
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
		return nil, fmt.Errorf("lock %s: %w (another repo service may already be running)", path, err)
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
