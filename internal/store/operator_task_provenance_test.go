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
// It has to be per task because it OUTLIVES the worker: claimed_by_worker_id
// is a live claim, the reaper deletes worker rows once they stop heartbeating,
// and a finished task releases its claim -- so a done, failed, or reaped task
// has no worker row left to ask. Provenance lives in the task's own metadata
// and still answers "what ran it?" afterwards.
//
// Within one native fan-out group these values agree by construction (one
// turn, one binding, recorded onto every task in the group), so the point is
// durability across time, not disagreement within a group.
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

// TestOperatorTaskProvenanceOutlivesItsWorker proves the justification above
// rather than merely asserting it in a comment.
//
// This is the whole reason the field is projected per task: the reaper DELETEs
// worker rows once they stop heartbeating (reaper.go), so after that
// claimed_by_worker_id names a row that no longer exists and cannot be joined
// to a provider. If provenance vanished with it, the operator would lose the
// answer for exactly the tasks most worth asking about -- the finished and the
// abandoned ones.
func TestOperatorTaskProvenanceOutlivesItsWorker(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "provenance-outlives-worker")
	planID, taskID, sessionID := seedRunningClaimedTask(t, s, projectID)
	if err := s.RecordTaskExecution(
		ctx, planID, taskID, "primary", "codex", "gpt-5", "high", "domain-a", sessionID,
	); err != nil {
		t.Fatalf("RecordTaskExecution: %v", err)
	}

	if _, err := s.DB().ExecContext(ctx, `DELETE FROM workers`); err != nil {
		t.Fatalf("delete workers: %v", err)
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d task(s), want 1: the task must survive its worker", len(items))
	}
	if got := items[0].AssignedProvider; got != "codex" {
		t.Fatalf("AssignedProvider = %q after the worker was reaped, want %q: "+
			"provenance that dies with the worker cannot answer what ran a "+
			"finished or abandoned task, which is the case it exists for", got, "codex")
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
