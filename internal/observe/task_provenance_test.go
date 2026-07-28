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
