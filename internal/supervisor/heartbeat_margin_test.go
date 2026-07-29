package supervisor

import (
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/orch"
)

// TestHeartbeatClearsTheStalenessWindow pins the margin between a running
// worker's heartbeat cadence and the age at which the reaper calls that worker
// abandoned. Nothing guarded it: workerHeartbeatInterval appeared in no test at
// all, and staleAfter appeared in supervisor tests only as a SCALING FACTOR
// (clock.Advance(staleAfter * 3)), which stays self-consistent for any value --
// it would keep passing even if the margin inverted.
//
// The invariant was documented in prose instead. orchestrator.go says the
// interval "MUST be well below the supervisor's staleAfter (90s)" and hardcodes
// that 90s in a comment, so changing staleAfter here silently turns that
// sentence into a lie with no test failing.
//
// What breaks if it inverts: the reaper tick runs CONCURRENTLY with provider
// turns (async dispatch), so a healthy worker mid-turn whose heartbeat lands
// outside the window gets its task requeued and re-dispatched to a second
// worker -- the task runs twice. That is the regression async dispatch
// introduced and this margin exists to prevent.
func TestHeartbeatClearsTheStalenessWindow(t *testing.T) {
	if orch.WorkerHeartbeatInterval >= staleAfter {
		t.Fatalf("heartbeat interval %v must be below staleAfter %v, or the "+
			"reaper requeues healthy workers mid-turn",
			orch.WorkerHeartbeatInterval, staleAfter)
	}
	// Several beats must fit inside the window, not merely one: a single
	// dropped or delayed beat must not be enough to look like a crash.
	if beats := staleAfter / orch.WorkerHeartbeatInterval; beats < 4 {
		t.Errorf("only %d heartbeats fit within staleAfter (%v / %v); want >= 4 "+
			"so one slow beat is not mistaken for a dead worker",
			beats, staleAfter, orch.WorkerHeartbeatInterval)
	}
	// staleAfter must likewise clear the reaper's own tick, or a merely-slow
	// tick reads as a crash.
	if staleAfter <= 2*reaperInterval {
		t.Errorf("staleAfter (%v) must exceed several reaperInterval (%v)",
			staleAfter, reaperInterval)
	}
}
