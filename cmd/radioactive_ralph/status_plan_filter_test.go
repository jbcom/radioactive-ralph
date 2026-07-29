package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// TestStatusPassesThePlanFilter guards the flag that makes a recent run
// readable at all.
//
// Task rows are capped at MaxOperatorPageLimit ACROSS EVERY PLAN, so a project
// with history cannot show a recent run: a completed 12-task run displayed 6
// rows, and neither cursor could recover the rest, because other plans' rows
// consume the page first. Scoping to one plan returned all 12.
//
// SnapshotQuery already carried PlanID and `messages` already exposed --plan;
// only `status`, where it matters most, did not. The flag has to reach the
// query -- a flag parsed into a variable nothing sends is the same defect as
// the unwired store methods this came out of.
func TestStatusPassesThePlanFilter(t *testing.T) {
	root := newStatusCmd()
	if f := root.Flags().Lookup("plan"); f == nil {
		t.Fatal("status has no --plan flag; a saturated task page cannot be " +
			"scoped to one run, and cursors cannot isolate a plan")
	}

	client := &fakeObserveClient{snapshot: querySnapshotFixture(1)}
	var out strings.Builder
	if err := runStatusQueryWith(
		context.Background(), &out, client,
		ipc.ObserveSnapshotArgs{ProjectID: "project-1", PlanID: "plan-xyz"},
		true, false,
	); err != nil {
		t.Fatalf("runStatusQueryWith: %v", err)
	}
	if client.snapshotQ.PlanID != "plan-xyz" {
		t.Errorf("query reached the supervisor with PlanID = %q, want %q -- the "+
			"flag is parsed but never sent, so scoping silently does nothing",
			client.snapshotQ.PlanID, "plan-xyz")
	}
}
