//go:build windows

package agent

import "os"

// killProcessTree kills the process. On Windows the pty-backed agent path is
// unsupported (Start returns ErrPTYUnsupported before any child is spawned), so
// there is no session/process-group tree to reap here — the direct kill is the
// correct and only behavior. (A native ConPTY agent path, if added later, would
// need a Job Object or CREATE_NEW_PROCESS_GROUP + taskkill /T to reap children.)
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
