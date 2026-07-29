package orch

import (
	"errors"
	"strings"
	"testing"
)

// TestFailureDecisionRecordsConcurrentLoad guards the measure a reviewer showed
// was missing.
//
// I compared runs by reclaim_count and wrote "0 reclaims = no contention".
// That is the wrong number for the question: reclaim_count increments only on a
// stale heartbeat or an orphaned claim, and this plan makes NINE steps ready at
// once after `build` -- so nine processes can saturate CPU and I/O while it
// stays at 0. "No worker died" is not "the machine was idle".
//
// A failure record therefore needs the load AT FAILURE TIME, not a proxy that
// answers a different question. Without it, the contention cofactor cannot be
// confirmed or dismissed from the artifact -- only guessed at, which is how
// this investigation already burned three wrong explanations.
func TestFailureDecisionRecordsConcurrentLoad(t *testing.T) {
	got := failureDecisionLine("interactive_prompt", "provider requested operator input", 9, nil)
	if !strings.Contains(got, "interactive_prompt") {
		t.Errorf("decision = %q, want the category", got)
	}
	// The NUMBER is the point. A line naming the category without the load
	// leaves the cofactor unmeasurable, which is the state this fixes.
	if !strings.Contains(got, "9 workers running") {
		t.Errorf("decision = %q, want the concurrent worker count at failure time "+
			"-- without it, contention can only be guessed at", got)
	}

	// A solo failure must say so plainly rather than omitting the field, so an
	// absent number never has to be read as "unknown".
	solo := failureDecisionLine("interactive_prompt", "x", 1, nil)
	if !strings.Contains(solo, "1 worker running") {
		t.Errorf("solo decision = %q, want it to state the count is 1", solo)
	}
}

// TestFailureDecisionDistinguishesUnknownLoad keeps a failed measurement from
// reading as a measured zero.
//
// The first version discarded CountRunningWorkers' error, so a count that
// failed -- an expired context, a busy SQLite -- left running == 0 and wrote a
// confident "0 workers running". That is the same false confidence that made
// reclaim_count look like evidence about contention: an unmeasured value
// presented as a measured one.
func TestFailureDecisionDistinguishesUnknownLoad(t *testing.T) {
	got := failureDecisionLine("stall_timeout", "no progress", 0, errors.New("db busy"))
	if strings.Contains(got, "0 workers running") {
		t.Errorf("decision = %q, want it NOT to claim zero load for a count that "+
			"failed -- an unmeasured value must not read as a measured one", got)
	}
	if !strings.Contains(got, "unavailable") {
		t.Errorf("decision = %q, want it to say the count is unavailable", got)
	}
}
