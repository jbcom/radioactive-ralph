//go:build windows

package provider

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// snapshotAuthoritativeResultInfo captures the file identity eagerly through a
// stable, non-following handle. Windows FileInfo returned by os.Lstat loads its
// file ID lazily, which would otherwise resolve a replacement path only when
// os.SameFile is called after the race window.
func snapshotAuthoritativeResultInfo(path string) (os.FileInfo, error) {
	file, err := openAuthoritativeResultFile(path)
	if err != nil {
		return nil, errors.Join(ErrAuthoritativeResultUnsafe, err)
	}
	defer func() { _ = file.Close() }()
	return file.Stat()
}

func openAuthoritativeResultFile(path string) (*os.File, error) {
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
