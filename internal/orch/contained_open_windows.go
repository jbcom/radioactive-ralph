//go:build windows

package orch

import (
	"os"

	"golang.org/x/sys/windows"
)

// openContainedFile opens path for reading without traversing a reparse point,
// the Windows equivalent of the Unix O_NOFOLLOW open. FILE_FLAG_OPEN_REPARSE_POINT
// makes CreateFile hand back the link itself rather than its target, so a
// junction or symlink at a declared path cannot redirect the read.
//
// Mirrors internal/provider/result_open_windows.go.
func openContainedFile(path string) (*os.File, error) {
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathUTF16,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
