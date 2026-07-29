//go:build darwin || linux

package provider

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func snapshotAuthoritativeResultInfo(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func openAuthoritativeResultFile(path string) (*os.File, error) {
	fd, err := unix.Open(
		path,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, err
	}
	if fd < 0 {
		return nil, fmt.Errorf("provider: open %s returned invalid file descriptor %d", path, fd)
	}
	return os.NewFile(uintptr(fd), path), nil
}
