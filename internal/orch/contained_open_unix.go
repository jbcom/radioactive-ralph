//go:build darwin || linux

package orch

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openContainedFile opens path for reading WITHOUT following a final symlink,
// so the bytes a caller then reads are the bytes of the inode named by that
// exact path — not of whatever a link points at by the time the read happens.
//
// This mirrors internal/provider/result_open_unix.go rather than inventing a
// second pattern. O_NONBLOCK keeps the open from parking on a fifo or device
// that happens to sit at a declared path.
func openContainedFile(path string) (*os.File, error) {
	fd, err := unix.Open(
		path,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, err
	}
	if fd < 0 {
		return nil, fmt.Errorf("orch: open %s returned invalid file descriptor %d", path, fd)
	}
	return os.NewFile(uintptr(fd), path), nil
}
