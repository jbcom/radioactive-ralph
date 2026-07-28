package store

import (
	"context"
	"errors"
	"testing"
)

// seedMetadataTask creates a plan + task and its immutable metadata row.
func seedMetadataTask(t *testing.T, s *Store, projectID, slug, taskID, groupPath, teamPath string) string {
	t.Helper()
	ctx := context.Background()
	planID := mustCreatePlan(t, s, projectID, slug)
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: taskID, Description: taskID}); err != nil {
		t.Fatalf("CreateTask %s: %v", taskID, err)
	}
	if err := s.PutTaskMetadata(ctx, planID, taskID, groupPath, teamPath, "{}"); err != nil {
		t.Fatalf("PutTaskMetadata %s: %v", taskID, err)
	}
	return planID
}

// TestTaskMetadataRoundTripsGroupPath pins the field dispatch depends on.
// group_path carries each task's leaf-group identity so a ready wave can be
// partitioned before native fan-out; if it did not survive a write/read cycle,
// fan-out would silently group unrelated tasks under one provider.
func TestTaskMetadataRoundTripsGroupPath(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "meta-roundtrip")
	planID := seedMetadataTask(t, s, projectID, "p-roundtrip", "task-a", "0.2", "team/alpha")

	got, err := s.GetTaskExecutionMetadata(ctx, planID, "task-a")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if got.GroupPath != "0.2" {
		t.Errorf("GroupPath = %q, want %q", got.GroupPath, "0.2")
	}
	if got.TeamPath != "team/alpha" {
		t.Errorf("TeamPath = %q, want %q", got.TeamPath, "team/alpha")
	}
}

// TestPutTaskMetadataRequiresGroupPath fails closed rather than persisting a
// task that dispatch cannot place in a partition.
func TestPutTaskMetadataRequiresGroupPath(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "meta-nogroup")
	planID := mustCreatePlan(t, s, projectID, "p-nogroup")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t1", Description: "t1"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.PutTaskMetadata(ctx, planID, "t1", "", "team/a", "{}"); err == nil {
		t.Fatal("PutTaskMetadata accepted an empty group path")
	}
}

// TestListTaskGroupPathsReturnsEveryTask proves dispatch can partition a whole
// ready wave with one query instead of a lookup per task.
func TestListTaskGroupPathsReturnsEveryTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "meta-list")
	planID := seedMetadataTask(t, s, projectID, "p-list", "a", "0.0", "team/a")
	for _, tc := range []struct{ id, group string }{{"b", "0.0"}, {"c", "0.1"}} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: tc.id, Description: tc.id}); err != nil {
			t.Fatalf("CreateTask %s: %v", tc.id, err)
		}
		if err := s.PutTaskMetadata(ctx, planID, tc.id, tc.group, "team/a", "{}"); err != nil {
			t.Fatalf("PutTaskMetadata %s: %v", tc.id, err)
		}
	}

	got, err := s.ListTaskGroupPaths(ctx, planID)
	if err != nil {
		t.Fatalf("ListTaskGroupPaths: %v", err)
	}
	want := map[string]string{"a": "0.0", "b": "0.0", "c": "0.1"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for id, group := range want {
		if got[id] != group {
			t.Errorf("group for %q = %q, want %q", id, got[id], group)
		}
	}
}

// TestBlockedStatusesFoldIntoOneCount pins that the two new fail-closed blocks
// aggregate with the dependency block. An operator asking "how many tasks are
// blocked" wants one number; three separate causes must not report as zero.
func TestBlockedStatusesFoldIntoOneCount(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "blocked-fold")
	planID := mustCreatePlan(t, s, projectID, "p-blocked")
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE plans SET status='active' WHERE id=?`, planID); err != nil {
		t.Fatalf("activate plan: %v", err)
	}

	for _, tc := range []struct {
		id     string
		status TaskStatus
	}{
		{"b1", TaskStatusBlocked},
		{"b2", TaskStatusBlockedCapability},
		{"b3", TaskStatusBlockedInput},
	} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: tc.id, Description: tc.id}); err != nil {
			t.Fatalf("CreateTask %s: %v", tc.id, err)
		}
		if _, err := s.DB().ExecContext(ctx,
			`UPDATE tasks SET status=? WHERE plan_id=? AND id=?`, string(tc.status), planID, tc.id); err != nil {
			t.Fatalf("set status %s: %v", tc.id, err)
		}
	}

	counts, err := s.StatusCounts(ctx)
	if err != nil {
		t.Fatalf("StatusCounts: %v", err)
	}
	if counts.Blocked != 3 {
		t.Fatalf("Blocked = %d, want 3 (dependency + capability + input)", counts.Blocked)
	}
}

// TestMarkBlockedRecordsReasonAndStatus covers both fail-closed pre-dispatch
// blocks: the task status moves and the reason lands on the metadata row.
func TestMarkBlockedRecordsReasonAndStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "meta-blocked")

	for _, tc := range []struct {
		name   string
		slug   string
		status TaskStatus
	}{
		{"capability", "p-cap", TaskStatusBlockedCapability},
		{"input", "p-in", TaskStatusBlockedInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planID := seedMetadataTask(t, s, projectID, tc.slug, "t", "0.0", "team/a")
			var err error
			if tc.status == TaskStatusBlockedCapability {
				_, err = s.MarkBlockedCapability(ctx, planID, "t", "missing capability")
			} else {
				_, err = s.MarkBlockedInput(ctx, planID, "t", "input admission failed")
			}
			if err != nil {
				t.Fatalf("mark blocked: %v", err)
			}

			var status string
			if err := s.DB().QueryRowContext(ctx,
				`SELECT status FROM tasks WHERE plan_id=? AND id=?`, planID, "t").Scan(&status); err != nil {
				t.Fatalf("read status: %v", err)
			}
			if TaskStatus(status) != tc.status {
				t.Errorf("status = %q, want %q", status, tc.status)
			}

			meta, err := s.GetTaskExecutionMetadata(ctx, planID, "t")
			if err != nil {
				t.Fatalf("GetTaskExecutionMetadata: %v", err)
			}
			if meta.BlockedReason == "" {
				t.Error("BlockedReason is empty; the operator loses the cause")
			}
		})
	}
}

// TestMarkBlockedFailsClosedWithoutMetadata covers the task that predates
// migration 0003. The migration does not backfill task_metadata, so such a task
// has no row to record a reason on. Without a RowsAffected check the status
// update would commit while the reason was discarded, leaving a task that is
// unclaimable for no visible cause — worse than refusing outright.
func TestMarkBlockedFailsClosedWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "meta-absent")
	planID := mustCreatePlan(t, s, projectID, "p-absent")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "bare", Description: "bare"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Deliberately no PutTaskMetadata.

	_, err := s.MarkBlockedCapability(ctx, planID, "bare", "missing capability")
	if !errors.Is(err, ErrTaskMetadataMissing) {
		t.Fatalf("MarkBlockedCapability = %v, want ErrTaskMetadataMissing", err)
	}

	// The transaction must have rolled back: the task stays claimable rather
	// than being stranded in a blocked state with no recorded reason.
	task, err := s.GetTask(ctx, planID, "bare")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusPending {
		t.Fatalf("status = %q, want pending (the failed block must roll back)", task.Status)
	}
}

// TestTeamRollupsCountsDistinctWorkers pins that ActiveWorkers counts workers,
// not running tasks. Native fan-out lets one worker claim several tasks in the
// same team, so counting per running row would report a worker holding three
// fan-out tasks as three workers — and the never-block invariant is judged
// against worker counts.
func TestTeamRollupsCountsDistinctWorkers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "rollup-workers")
	planID := mustCreatePlan(t, s, projectID, "p-rollup-workers")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	// One worker claims three tasks in the same team, as native fan-out does.
	for _, id := range []string{"f1", "f2", "f3"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
		if err := s.PutTaskMetadata(ctx, planID, id, "0.0", "team/fanout", "{}"); err != nil {
			t.Fatalf("PutTaskMetadata %s: %v", id, err)
		}
		if _, err := s.DB().ExecContext(ctx, `
			UPDATE tasks SET status='running', claimed_by_session=?, claimed_by_worker_id=?
			WHERE plan_id=? AND id=?`, sessionID, workerID, planID, id); err != nil {
			t.Fatalf("claim %s: %v", id, err)
		}
	}

	rollups, err := s.TeamRollups(ctx, projectID)
	if err != nil {
		t.Fatalf("TeamRollups: %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("got %d rollups, want 1", len(rollups))
	}
	if rollups[0].Running != 3 {
		t.Errorf("Running = %d, want 3 (three running tasks)", rollups[0].Running)
	}
	if rollups[0].ActiveWorkers != 1 {
		t.Fatalf("ActiveWorkers = %d, want 1 — one worker holding three fan-out tasks is ONE worker",
			rollups[0].ActiveWorkers)
	}
}

// TestErrTaskNotOwnedRunningWrapsErrTaskNotRunning pins the sentinel hierarchy.
// Callers that only care "this task is not in a live claim I own" match the
// broad sentinel; callers that need the narrower distinction still can.
func TestErrTaskNotOwnedRunningWrapsErrTaskNotRunning(t *testing.T) {
	if !errors.Is(ErrTaskNotOwnedRunning, ErrTaskNotRunning) {
		t.Fatal("ErrTaskNotOwnedRunning must wrap ErrTaskNotRunning")
	}
	if errors.Is(ErrTaskNotRunning, ErrTaskNotOwnedRunning) {
		t.Fatal("the wrap must be one-directional; ErrTaskNotRunning is the broader sentinel")
	}
}

// TestTeamRollupsAggregateByTeam proves the operator-facing rollup survives the
// task_metadata_view.go discard: it is consumed by the supervisor status path
// and the GUI team view, so dropping it with enrichTaskMetadata would have been
// a regression.
func TestTeamRollupsAggregateByTeam(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "rollups")
	planID := seedMetadataTask(t, s, projectID, "p-rollup", "a", "0.0", "team/alpha")
	for _, tc := range []struct{ id, team string }{{"b", "team/alpha"}, {"c", "team/beta"}} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: tc.id, Description: tc.id}); err != nil {
			t.Fatalf("CreateTask %s: %v", tc.id, err)
		}
		if err := s.PutTaskMetadata(ctx, planID, tc.id, "0.0", tc.team, "{}"); err != nil {
			t.Fatalf("PutTaskMetadata %s: %v", tc.id, err)
		}
	}

	rollups, err := s.TeamRollups(ctx, projectID)
	if err != nil {
		t.Fatalf("TeamRollups: %v", err)
	}
	if len(rollups) != 2 {
		t.Fatalf("got %d rollups, want 2: %+v", len(rollups), rollups)
	}
	// Sorted by team path, so alpha precedes beta deterministically.
	if rollups[0].TeamPath != "team/alpha" || rollups[0].Total != 2 {
		t.Errorf("rollups[0] = %+v, want team/alpha with Total 2", rollups[0])
	}
	if rollups[1].TeamPath != "team/beta" || rollups[1].Total != 1 {
		t.Errorf("rollups[1] = %+v, want team/beta with Total 1", rollups[1])
	}
}
