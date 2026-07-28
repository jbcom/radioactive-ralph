package store

import (
	"context"
	"testing"
)

// seedRunningClaimedTask creates one task and claims it, the state
// RecordTaskExecution requires: it only writes provenance for a task that is
// running AND claimed by the recording session, so an unclaimed task would
// silently record nothing and make this test pass for the wrong reason.
func seedRunningClaimedTask(t *testing.T, s *Store, projectID string) (planID, taskID, sessionID string) {
	t.Helper()
	ctx := context.Background()
	taskID = "task-a"
	planID = seedMetadataTask(t, s, projectID, "p-provenance", taskID, "0", "team/alpha")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, taskID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	return planID, taskID, sessionID
}

// operatorTasksForTest reads the per-task operator projection through the
// public entry point, so the test pins the surface an operator actually sees
// rather than an internal query it could drift from.
func operatorTasksForTest(ctx context.Context, s *Store, projectID string) ([]OperatorTask, error) {
	snap, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return snap.Tasks.Items, nil
}

// TestOperatorTasksCarryExecutionProvenance pins the half of the observe
// surface that the provenance gate opened and nobody walked through.
//
// RecordTaskExecution has persisted assigned_alias, assigned_provider, and
// assigned_independence_domain since the calibration work landed, and
// TeamRollups already aggregates them. But the PER-TASK operator projection
// never selected them, so the one question provenance exists to answer --
// "which provider actually ran THIS task?" -- was answerable only in aggregate
// or by raw SQLite, the access the dumb-client boundary removes.
//
// The distinction matters most exactly where aggregates are useless: under
// native fan-out one worker owns several tasks in a partition, so a team-level
// provider count cannot tell you which task a given provider executed. A
// rollup saying "3 codex, 2 claude" is consistent with every assignment of
// those five tasks.
func TestOperatorTasksCarryExecutionProvenance(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-task-provenance")
	planID, taskID, sessionID := seedRunningClaimedTask(t, s, projectID)

	if err := s.RecordTaskExecution(
		ctx, planID, taskID, "primary", "codex", "gpt-5", "high", "domain-a", sessionID,
	); err != nil {
		t.Fatalf("RecordTaskExecution: %v", err)
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d task(s), want 1: %+v", len(items), items)
	}
	got := items[0]

	for _, f := range []struct{ name, got, want string }{
		{"AssignedAlias", got.AssignedAlias, "primary"},
		{"AssignedProvider", got.AssignedProvider, "codex"},
		{"AssignedModel", got.AssignedModel, "gpt-5"},
		{"AssignedEffort", got.AssignedEffort, "high"},
		{"AssignedIndependenceDomain", got.AssignedIndependenceDomain, "domain-a"},
	} {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q: provenance is recorded in task_metadata but "+
				"never projected per task, so an operator cannot tell which provider ran "+
				"this task without raw SQLite", f.name, f.got, f.want)
		}
	}
}

// TestOperatorTasksLeaveProvenanceEmptyBeforeExecution is the other half.
//
// A task that has not run yet has no provenance, and that must read as EMPTY
// rather than as some default provider name. Inventing a value here would make
// "not dispatched yet" indistinguishable from "ran on the pool default" -- the
// same absence-of-evidence confusion differentFrom refuses, and the reason
// unpinned tasks key separately when partitioning.
func TestOperatorTasksLeaveProvenanceEmptyBeforeExecution(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-task-no-provenance")
	seedRunningClaimedTask(t, s, projectID)

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d task(s), want 1", len(items))
	}
	got := items[0]
	if got.AssignedProvider != "" || got.AssignedAlias != "" ||
		got.AssignedIndependenceDomain != "" {
		t.Fatalf("unexecuted task reports provenance alias=%q provider=%q domain=%q, "+
			"want all empty: a task that never ran must not be indistinguishable from "+
			"one that ran on a default", got.AssignedAlias, got.AssignedProvider,
			got.AssignedIndependenceDomain)
	}
}
