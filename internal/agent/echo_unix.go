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
func disablePTYEcho(ptmx *os.File) error {
	termios, err := unix.IoctlGetTermios(int(ptmx.Fd()), unix.TIOCGETA)
	if err != nil {
		return err
	}
	// Clear the echo bits; leave the rest of the line discipline intact.
	termios.Lflag &^= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ECHONL
	return unix.IoctlSetTermios(int(ptmx.Fd()), unix.TIOCSETA, termios)
}
