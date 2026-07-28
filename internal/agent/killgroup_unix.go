//go:build !windows

package agent

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

// cleanupExitedProcessTree kills any descendants that remain in the PTY
// session after the leader's natural exit. ESRCH is success: no live session
// member remains. The caller holds processLifecycle.mu while the unreaped
// leader keeps its PID/PGID reserved.
func cleanupExitedProcessTree(process *os.Process) error {
	if process == nil || process.Pid <= 1 {
		return nil
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	// Darwin reports EPERM when the only remaining group member is the
	// already-observed zombie leader. If any live same-UID descendant remains,
	// that descendant is signalable and the group kill succeeds. Linux reports
	// ESRCH for the empty-live-group case, so retain EPERM as a real failure
	// there.
	if errors.Is(err, syscall.ESRCH) ||
		(runtime.GOOS == "darwin" && errors.Is(err, syscall.EPERM)) {
		err = nil
	}
	return errors.Join(err, cleanupOriginalProcessSession(process))
}

// terminateProcessTree first signals the whole PTY process group. If group
// cleanup fails, it falls back to os.Process.Kill for the direct child: Go's
// stable process handle/signal synchronization cannot target a recycled PID.
// A successful fallback proves direct-child termination but retains the group
// failure as ErrProcessTreeCleanup.
func terminateProcessTree(process *os.Process) terminationOutcome {
	return terminateProcessTreeWithGroupSignal(process, syscall.Kill)
}

func terminateProcessTreeWithGroupSignal(
	process *os.Process,
	signalGroup func(int, syscall.Signal) error,
) terminationOutcome {
	if process == nil {
		return terminationOutcome{}
	}
	if process.Pid > 1 {
		if err := signalGroup(-process.Pid, syscall.SIGKILL); err == nil {
			return terminationOutcome{
				cleanupErr: wrapSessionCleanup(
					cleanupOriginalProcessSession(process),
				),
			}
		} else if !errors.Is(err, syscall.ESRCH) {
			directErr := process.Kill()
			if directErr == nil || errors.Is(directErr, os.ErrProcessDone) {
				// On Darwin, signalling a group whose only member has already
				// become a zombie returns EPERM even though no live descendant
				// remains. Re-probe the group after the stable direct-handle
				// signal: EPERM/ESRCH here proves there is no signalable
				// same-UID descendant to reclaim. A live descendant makes the
				// group probe succeed, preserving the cleanup failure.
				if runtime.GOOS == "darwin" && errors.Is(err, syscall.EPERM) {
					groupProbeErr := signalGroup(-process.Pid, 0)
					if errors.Is(groupProbeErr, syscall.EPERM) ||
						errors.Is(groupProbeErr, syscall.ESRCH) {
						return terminationOutcome{
							cleanupErr: wrapSessionCleanup(
								cleanupOriginalProcessSession(process),
							),
						}
					}
				}
				return terminationOutcome{
					cleanupErr: errors.Join(
						ErrProcessSessionCleanup,
						err,
						cleanupOriginalProcessSession(process),
					),
				}
			}
			return terminationOutcome{
				cleanupErr: errors.Join(ErrProcessSessionCleanup, err),
				terminationErr: errors.Join(
					ErrProcessTermination,
					directErr,
				),
			}
		}
		// ESRCH means no group was found. The direct child may still be
		// signalable through its stable os.Process identity.
	}
	directErr := process.Kill()
	if directErr == nil || errors.Is(directErr, os.ErrProcessDone) {
		return terminationOutcome{
			cleanupErr: wrapSessionCleanup(
				cleanupOriginalProcessSession(process),
			),
		}
	}
	return terminationOutcome{terminationErr: errors.Join(ErrProcessTermination, directErr)}
}

func wrapSessionCleanup(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrProcessSessionCleanup, err)
}
