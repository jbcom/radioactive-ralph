package observe

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestTaskProjectsExecutionProvenance covers the second half of the hop the
// store test cannot see. The store query selecting the columns and the observe
// projection copying them are separate edits, and a field added to one but not
// the other produces a snapshot that is silently missing provenance while every
// store-level test still passes.
func TestTaskProjectsExecutionProvenance(t *testing.T) {
	got, err := taskFromStore(store.OperatorTask{
		PlanID:                     "plan-1",
		ID:                         "task-1",
		Status:                     store.TaskStatusRunning,
		AssignedAlias:              "primary",
		AssignedProvider:           "codex",
		AssignedModel:              "gpt-5",
		AssignedEffort:             "high",
		AssignedIndependenceDomain: "domain-a",
	})
	if err != nil {
		t.Fatalf("taskFromStore: %v", err)
	}

	for _, f := range []struct{ name, got, want string }{
		{"AssignedAlias", got.AssignedAlias, "primary"},
		{"AssignedProvider", got.AssignedProvider, "codex"},
		{"AssignedModel", got.AssignedModel, "gpt-5"},
		{"AssignedEffort", got.AssignedEffort, "high"},
		{"AssignedIndependenceDomain", got.AssignedIndependenceDomain, "domain-a"},
	} {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q: the store selects this column but the observe "+
				"projection drops it, so the operator surface reports no provenance",
				f.name, f.got, f.want)
		}
	}
}

// TestTaskProjectsPartitionOrdinal covers the same store->observe hop for the
// partition half. The store selecting it and the projection copying it are
// separate edits, so a field added to one but not the other yields a snapshot
// silently missing the grouping while every store test still passes.
func TestTaskProjectsPartitionOrdinal(t *testing.T) {
	got, err := taskFromStore(store.OperatorTask{
		PlanID:           "plan-1",
		ID:               "task-1",
		Status:           store.TaskStatusRunning,
		PartitionOrdinal: "a1b2c3d4e5f60718",
	})
	if err != nil {
		t.Fatalf("taskFromStore: %v", err)
	}
	if got.PartitionOrdinal != "a1b2c3d4e5f60718" {
		t.Errorf("PartitionOrdinal = %q, want %q: the store computes it but the "+
			"observe projection drops it, so an operator cannot see which tasks "+
			"one fan-out turn owns", got.PartitionOrdinal, "a1b2c3d4e5f60718")
	}
}

// TestTaskOmitsProvenanceBeforeExecution pins the wire shape, not just the Go
// field. omitempty is the load-bearing part: a task that has not run must be
// ABSENT from the JSON rather than present as "assigned_provider": "", which a
// client would otherwise have to special-case to avoid rendering an empty
// string as a provider name.
func TestTaskOmitsProvenanceBeforeExecution(t *testing.T) {
	got, err := taskFromStore(store.OperatorTask{
		PlanID: "plan-1",
		ID:     "task-1",
		Status: store.TaskStatusReady,
	})
	if err != nil {
		t.Fatalf("taskFromStore: %v", err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		"assigned_alias", "assigned_provider", "assigned_model",
		"assigned_effort", "assigned_independence_domain",
	} {
		if strings.Contains(string(raw), key) {
			t.Errorf("%q present for a task that never ran: %s\n"+
				"absent provenance must be omitted, not emitted empty, so a client "+
				"cannot mistake \"\" for a provider name", key, raw)
		}
	}
}

// TestPartitionLabelsMarkOnlyFanoutGroups pins the display policy now that it
// is shared by every renderer. Both rules are load-bearing: labelling
// singletons buries the real fan-out groups, and labelling nothing makes the
// grouping invisible.
func TestPartitionLabelsMarkOnlyFanoutGroups(t *testing.T) {
	labels := PartitionLabels([]Task{
		{ID: "a", PartitionOrdinal: "aaaa"},
		{ID: "b", PartitionOrdinal: "aaaa"},
		{ID: "c", PartitionOrdinal: "bbbb"}, // partition of one
		{ID: "d"},                           // no ordinal at all
	})
	if labels["aaaa"] == "" {
		t.Error("the partition holding two tasks got no label, so an operator " +
			"cannot see they are one fan-out turn")
	}
	if labels["bbbb"] != "" {
		t.Errorf("a partition of one was labelled %q; singletons are the ordinary "+
			"case and marking them buries the real groups", labels["bbbb"])
	}
	if labels[""] != "" {
		t.Errorf("tasks with no ordinal were labelled %q; absent metadata is not "+
			"evidence of a shared partition", labels[""])
	}
}

// TestPartitionLabelsNumberInDisplayOrder keeps the labels readable: the first
// fan-out group a reader meets is p1. Numbering by map iteration would reshuffle
// them between renders of identical data.
func TestPartitionLabelsNumberInDisplayOrder(t *testing.T) {
	tasks := []Task{
		{ID: "a", PartitionOrdinal: "zzz"},
		{ID: "b", PartitionOrdinal: "zzz"},
		{ID: "c", PartitionOrdinal: "aaa"},
		{ID: "d", PartitionOrdinal: "aaa"},
	}
	for i := range 12 {
		labels := PartitionLabels(tasks)
		if labels["zzz"] != "p1" || labels["aaa"] != "p2" {
			t.Fatalf("run %d: got zzz=%q aaa=%q, want p1/p2 in first-seen order",
				i, labels["zzz"], labels["aaa"])
		}
	}
}

// TestWorkerSuffixKeepsIdsDistinguishable pins the property the abbreviation
// exists for, and that a first attempt destroyed.
//
// Worker ids share a constant head, so truncating from the FRONT rendered every
// row as "worker-…" -- correlating nothing, which is the marker's only job.
// Caught by reading the rendered output; no width assertion would have noticed,
// since the broken version was exactly as narrow as the working one.
func TestWorkerSuffixKeepsIdsDistinguishable(t *testing.T) {
	// Real ids from store.CreateWorker are uuid-like with a longer shared
	// prefix than "worker-". A first version of this test used ids that
	// diverged at rune 8 and PASSED against front-truncation -- weaker than the
	// defect it claimed to guard.
	a := WorkerSuffix("worker-session-01-7f3a2b1c", 8)
	b := WorkerSuffix("worker-session-01-9e8d7c6b", 8)
	if a == b {
		t.Fatalf("two different workers both render as %q; the marker cannot "+
			"correlate rows if every id abbreviates identically", a)
	}
	if short := WorkerSuffix("w-1", 8); short != "w-1" {
		t.Errorf("an id shorter than the limit was altered: %q", short)
	}
}
