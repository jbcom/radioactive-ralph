//go:build linux

package agent

import "golang.org/x/sys/unix"

// Linux terminal get/set ioctl request numbers.
const (
	termiosGetReq = unix.TCGETS
	termiosSetReq = unix.TCSETS
)
