//go:build darwin || linux

package adapters

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openReleaseFile(path string) (*os.File, error) {
	fd, err := unix.Open(
		path,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, err
	}
	if fd < 0 {
		return nil, fmt.Errorf("adapters: open release file returned invalid descriptor")
	}
	return os.NewFile(uintptr(fd), path), nil
}
