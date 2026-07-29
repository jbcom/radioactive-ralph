package orch

import (
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
	got := failureDecisionLine("interactive_prompt", "provider requested operator input", 9)
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
	solo := failureDecisionLine("interactive_prompt", "x", 1)
	if !strings.Contains(solo, "1 worker running") {
		t.Errorf("solo decision = %q, want it to state the count is 1", solo)
	}
}
