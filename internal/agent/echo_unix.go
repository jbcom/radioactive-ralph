//go:build !windows

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

// disablePTYEcho clears the ECHO (and related echo) flags on the pty's line
// discipline so bytes written to the pty master are NOT reflected back onto
// the master's read side. Without this the pty echoes every stdin line the
// provider sends, and the never-block watchdog pattern-matches the operator's
// own prompt text as an interactive prompt, killing an otherwise-valid turn.
//
// The get/set ioctl request numbers differ across Unix flavors (Linux uses
// TCGETS/TCSETS; the BSDs and macOS use TIOCGETA/TIOCSETA), so they are
// supplied per-platform via termiosGetReq/termiosSetReq.
func disablePTYEcho(ptmx *os.File) error {
	fd := int(ptmx.Fd())
	termios, err := unix.IoctlGetTermios(fd, termiosGetReq)
	if err != nil {
		return err
	}
	// Clear the echo bits; leave the rest of the line discipline intact.
	termios.Lflag &^= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ECHONL
	return unix.IoctlSetTermios(fd, termiosSetReq, termios)
}
