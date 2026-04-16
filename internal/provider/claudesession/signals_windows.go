//go:build windows

package claudesession

import "os"

// interruptSignal on Windows is os.Interrupt, which is effectively
// SIGTERM under the hood. Claude Code's Windows behavior on this
// signal is a regular shutdown rather than a mid-turn cancel, so the
// Claude provider gets coarser cancellation semantics on native
// Windows than it does on POSIX hosts.
var interruptSignal os.Signal = os.Interrupt
