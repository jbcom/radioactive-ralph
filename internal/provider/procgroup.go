package provider

import (
	"os"
	"os/exec"
)

// ConfigureProcessCancellation applies the provider package's platform
// cancellation behavior to cmd. Unix kills the complete process group and
// bounds inherited-pipe waits. Windows currently kills only the direct child
// and bounds the wait; callers that require complete descendant cleanup must
// fail closed there until Job Object support exists.
func ConfigureProcessCancellation(cmd *exec.Cmd) {
	setProcessGroupKill(cmd)
}

// KillProcessTree performs the provider package's platform cleanup for an
// already-started command. On Unix this kills the complete process group,
// including a descendant that still owns an inherited output pipe after the
// direct child exits. Windows callers get direct-process cleanup only.
func KillProcessTree(process *os.Process) error {
	return killProcessTree(process)
}
