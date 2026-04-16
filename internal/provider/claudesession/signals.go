//go:build !windows

package claudesession

import (
	"os"
	"syscall"
)

// interruptSignal is SIGINT on POSIX — Claude Code interprets it as
// an in-flight cancel rather than a hard kill.
var interruptSignal os.Signal = syscall.SIGINT
