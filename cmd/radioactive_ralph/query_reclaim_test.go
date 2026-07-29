package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// TestStatusRendersReclaimReason checks the ARTIFACT an operator reads, not the
// field that feeds it.
//
// A reclaim reason that reaches the JSON but never the human line leaves the
// CLI's default output strictly less informative than its own --json, which is
// the gap that made `reclaim_count: 2` require a hand-correlation against the
// events stream to interpret.
func TestStatusRendersReclaimReason(t *testing.T) {
	reply := querySnapshotFixture(1)
	reply.Tasks.Items = []observe.Task{
		{
			PlanID:                  "plan-1",
			ID:                      "race",
			Status:                  "running",
			ReclaimCount:            2,
			ReclaimReason:           "stale_heartbeat",
			ReclaimConcurrentClaims: 6,
		},
		{
			PlanID: "plan-1",
			ID:     "build",
			Status: "done",
		},
	}

	var out bytes.Buffer
	if err := runStatusQueryWith(
		context.Background(),
		&out,
		&fakeObserveClient{snapshot: reply},
		ipc.ObserveSnapshotArgs{ProjectID: "project-1"},
		false,
		false,
	); err != nil {
		t.Fatalf("runStatusQueryWith: %v", err)
	}

	got := out.String()
	var raceLine string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "race") {
			raceLine = line
			break
		}
	}
	if raceLine == "" {
		t.Fatalf("no row for the reclaimed task; output:\n%s", got)
	}
	if !strings.Contains(raceLine, "reclaimed 2x") {
		t.Errorf("reclaimed row = %q, want it to state the reclaim count", raceLine)
	}
	if !strings.Contains(raceLine, "stale_heartbeat") {
		t.Errorf("reclaimed row = %q, want it to name the reason -- a bare count "+
			"is the ambiguity this exists to remove", raceLine)
	}
	// The load is the actual suspect. "stale_heartbeat" is true and still sends
	// the reader to inspect the worker when six other steps were starving it.
	if !strings.Contains(raceLine, "6 claims in flight") {
		t.Errorf("reclaimed row = %q, want it to name the concurrency pressure", raceLine)
	}

	// A task that was never reclaimed must stay clean: a marker on every row is
	// noise that trains the reader to skip the column.
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "build") && strings.Contains(line, "reclaimed") {
			t.Errorf("un-reclaimed row carries a reclaim marker: %q", line)
		}
	}
}

// TestStatusRendersAttemptLabel pins the display policy in observe.AttemptLabel.
//
// The label states claims the task was GIVEN, which is lost claims plus the one
// in hand. "reclaimed 2x" says how many were taken AWAY -- a different number,
// and not one a reader should have to add to.
//
// Empty when nothing was lost: on a healthy task "1 attempt" is true of every
// row and marks nothing, so it would be noise on exactly the rows with no
// trouble to find.
func TestStatusRendersAttemptLabel(t *testing.T) {
	render := func(t *testing.T, task observe.Task) string {
		t.Helper()
		reply := querySnapshotFixture(1)
		reply.Tasks.Items = []observe.Task{task}
		var out bytes.Buffer
		if err := runStatusQueryWith(
			context.Background(), &out, &fakeObserveClient{snapshot: reply},
			ipc.ObserveSnapshotArgs{ProjectID: "project-1"}, false, false,
		); err != nil {
			t.Fatalf("runStatusQueryWith: %v", err)
		}
		return out.String()
	}

	// 1 retry + 2 reclaims = three lost claims, plus the one it holds = 4.
	both := render(t, observe.Task{
		PlanID: "plan-1", ID: "flaky", Status: "running",
		ReclaimCount: 2, ReclaimReason: "stale_heartbeat", RetryCount: 1,
	})
	if !strings.Contains(both, "4 attempts") {
		t.Errorf("1 retry + 2 reclaims does not state its 4 claims:\n%s", both)
	}

	// Reclaims alone still get a label: "reclaimed 2x" is claims LOST, not
	// claims given, so the reader would otherwise have to do the arithmetic.
	reclaimsOnly := render(t, observe.Task{
		PlanID: "plan-1", ID: "reaped", Status: "running",
		ReclaimCount: 2, ReclaimReason: "stale_heartbeat",
	})
	if !strings.Contains(reclaimsOnly, "3 attempts") {
		t.Errorf("2 reclaims does not state its 3 claims:\n%s", reclaimsOnly)
	}

	// A healthy task gets nothing: "1 attempt" on every row marks nothing.
	clean := render(t, observe.Task{PlanID: "plan-1", ID: "fine", Status: "done"})
	if strings.Contains(clean, "attempts") {
		t.Errorf("an untroubled task carries an attempt marker:\n%s", clean)
	}
}
