//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package agent

import "golang.org/x/sys/unix"

// BSD/macOS terminal get/set ioctl request numbers.
const (
	termiosGetReq = unix.TIOCGETA
	termiosSetReq = unix.TIOCSETA
)
