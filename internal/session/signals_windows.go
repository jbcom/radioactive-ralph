//go:build windows

package session

import "os"

// interruptSignal on Windows is os.Interrupt, which is effectively
// SIGTERM under the hood. Claude Code's Windows behavior on this
// signal is a regular shutdown rather than a mid-turn cancel; Ralph's
// supervisor does not manage sessions on native Windows (POSIX-only
// component), so this is largely for compile completeness.
var interruptSignal os.Signal = os.Interrupt
