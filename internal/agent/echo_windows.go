//go:build windows

package agent

import "os"

// disablePTYEcho is a no-op on native Windows: creack/pty is unsupported
// there (Start returns ErrPTYUnsupported before this is reached), so there is
// no pty line discipline to configure.
func disablePTYEcho(_ *os.File) error { return nil }
