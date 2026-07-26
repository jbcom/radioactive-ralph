package provider

import "os/exec"

// ConfigureProcessCancellation applies the provider package's platform
// cancellation behavior to cmd. Unix kills the complete process group and
// bounds inherited-pipe waits. Windows currently kills only the direct child
// and bounds the wait; callers that require complete descendant cleanup must
// fail closed there until Job Object support exists.
func ConfigureProcessCancellation(cmd *exec.Cmd) {
	setProcessGroupKill(cmd)
}
