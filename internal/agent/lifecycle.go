package agent

import (
	"errors"
	"os"
	"sync"
)

var errProcessExitObservationStopped = errors.New("agent: process exit observation stopped")

const maxTerminationAttempts = 3

type lifecyclePhase uint8

const (
	lifecycleRunning lifecyclePhase = iota
	lifecycleReaping
	lifecycleFinished
	lifecycleFailed
)

type terminationOutcome struct {
	alreadyExited  bool
	cleanupErr     error
	terminationErr error
}

type terminationClaim struct {
	claimed        bool
	natural        bool
	observationErr error
	cleanupErr     error
	terminationErr error
}

// processLifecycle is the sole linearization point for raw process operations
// and cmd.Wait ownership. Natural observation and explicit termination are
// distinct claim paths:
//
//   - claimNatural may run only after a non-reaping kernel observer proves exit.
//   - claimTermination signals the tree/direct child while holding mu and
//     transfers directly to reaping after successful termination ownership.
//
// Therefore neither output backpressure nor observer health is required after
// an explicit kill succeeds, and no raw PID/PGID operation can begin after
// cmd.Wait may reap/recycle the leader.
type processLifecycle struct {
	mu      sync.Mutex
	phase   lifecyclePhase
	forced  bool
	exitErr error
}

func (l *processLifecycle) claimTermination(
	observe func() (bool, error),
	terminate func() terminationOutcome,
) terminationClaim {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != lifecycleRunning {
		return terminationClaim{}
	}

	exited, observationErr := observe()
	if observationErr == nil && exited {
		return terminationClaim{natural: true}
	}

	var cleanupErr error
	var terminationErr error
	for range maxTerminationAttempts {
		outcome := terminate()
		cleanupErr = errors.Join(cleanupErr, outcome.cleanupErr)
		terminationErr = outcome.terminationErr
		if outcome.alreadyExited {
			return terminationClaim{
				natural:        true,
				observationErr: observationErr,
				cleanupErr:     cleanupErr,
			}
		}
		if terminationErr == nil {
			l.forced = true
			l.phase = lifecycleReaping
			return terminationClaim{
				claimed:        true,
				observationErr: observationErr,
				cleanupErr:     cleanupErr,
			}
		}

		// A stable direct-process handle may report a transient signal failure
		// while the child is concurrently reaching a terminal state. Re-probe
		// under the same lifecycle lock before retrying: this both preserves a
		// natural exit and prevents any raw observer probe from crossing
		// cmd.Wait ownership.
		exited, err := observe()
		observationErr = errors.Join(observationErr, err)
		if err == nil && exited {
			return terminationClaim{
				natural:        true,
				observationErr: observationErr,
				cleanupErr:     cleanupErr,
			}
		}
	}

	l.phase = lifecycleFailed
	return terminationClaim{
		observationErr: observationErr,
		cleanupErr:     cleanupErr,
		terminationErr: terminationErr,
	}
}

// observe is the only path by which a natural observer may make a raw
// PID/process-handle query. It linearizes that query against the transition to
// cmd.Wait ownership, so no probe can begin after the direct child is reaped.
func (l *processLifecycle) observe(probe func() (bool, error)) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != lifecycleRunning {
		return false, errProcessExitObservationStopped
	}
	return probe()
}

// claimNatural transfers a kernel-confirmed exited child to cmd.Wait. Cleanup
// failure is reportable but cannot make Wait block: the observer has already
// proved the direct child waitable.
func (l *processLifecycle) claimNatural(cleanup func() error) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != lifecycleRunning {
		return false, nil
	}
	err := cleanup()
	l.phase = lifecycleReaping
	return true, err
}

func (l *processLifecycle) finish(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != lifecycleReaping {
		return
	}
	l.exitErr = err
	l.forced = processWaitWasForced(err, l.forced)
	l.phase = lifecycleFinished
}

func (l *processLifecycle) exitError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != lifecycleFinished || l.forced {
		return nil
	}
	return l.exitErr
}

func (l *processLifecycle) snapshot() (lifecyclePhase, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.phase, l.forced, l.exitErr
}

func (l *processLifecycle) pid(process *os.Process) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.phase != lifecycleRunning || process == nil {
		return 0
	}
	return process.Pid
}
