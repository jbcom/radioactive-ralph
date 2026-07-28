//go:build darwin || linux

package agent

import (
	"testing"
	"time"
)

// TestSessionCleanupBudgetIsWallClockNotPollAttempts locks in the contract that
// reclamation is bounded by elapsed real time rather than by a poll-attempt
// count.
//
// Regression: the budget used to be sessionCleanupAttempts(100) *
// sessionCleanupInterval(5ms), described as "500ms". That identity only holds
// when each pass is free. Every pass also walks the whole process table
// (kern.proc.all on Darwin, /proc on Linux) plus a getsid(2) per process, so on
// an oversubscribed CI host the 100 attempts were consumed in far less than
// 500ms of real time — one measured run burned the entire budget in 13 passes.
// Cleanup then aborted while the tree was still converging and reported live
// members, wrapping ErrProcessSessionCleanup around the caller's real error and
// breaking static-sentinel comparisons such as
// OutputErr() == ErrObservedOutputTooLarge.
func TestSessionCleanupBudgetIsWallClockNotPollAttempts(t *testing.T) {
	// A poll interval that cannot on its own explain the budget: if the budget
	// were still expressed as a fixed number of sleeps, this ratio would make
	// the effective deadline collapse under load.
	if sessionCleanupInterval <= 0 {
		t.Fatalf("sessionCleanupInterval = %v, want a positive poll interval", sessionCleanupInterval)
	}
	if sessionCleanupBudget <= 0 {
		t.Fatalf("sessionCleanupBudget = %v, want a positive wall-clock budget", sessionCleanupBudget)
	}

	// Measured worst-case convergence for a 32-descendant PTY tree on a loaded
	// 16-core macOS host was ~1.1s. The budget must clear that with margin, or
	// the CI-only "still has live members after cleanup" failure returns.
	const measuredWorstCaseConvergence = 1100 * time.Millisecond
	if sessionCleanupBudget <= measuredWorstCaseConvergence {
		t.Fatalf(
			"sessionCleanupBudget = %v, want > %v (measured loaded-host convergence)",
			sessionCleanupBudget,
			measuredWorstCaseConvergence,
		)
	}

	// The supervisor must never block. Callers join a terminate-and-reap within
	// ~3s, so the budget has to stay comfortably under that ceiling.
	const callerJoinCeiling = 3 * time.Second
	if sessionCleanupBudget >= callerJoinCeiling {
		t.Fatalf(
			"sessionCleanupBudget = %v, want < %v to preserve the never-block invariant",
			sessionCleanupBudget,
			callerJoinCeiling,
		)
	}

	// The budget must not be re-derivable as attempts*interval; that arithmetic
	// is exactly the defect. Guard the old constant's value so a revert to
	// 100*5ms fails here rather than only under CI load.
	const revertedBudget = 100 * 5 * time.Millisecond
	if sessionCleanupBudget == revertedBudget {
		t.Fatalf(
			"sessionCleanupBudget = %v, which is the reverted attempts*interval value",
			sessionCleanupBudget,
		)
	}
}
