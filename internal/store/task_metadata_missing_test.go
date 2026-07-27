package store

import (
	"context"
	"errors"
	"testing"
)

// taskWithoutMetadata creates a plan and a task via the plain CreateTask path,
// so the task deliberately has NO task_metadata row. This is not a contrived
// state: migration 0003 does not backfill metadata for tasks that existed
// before it, so every pre-existing task is in exactly this shape.
func taskWithoutMetadata(t *testing.T, s *Store, slug string) (planID, taskID string) {
	t.Helper()
	ctx := context.Background()
	projectID := mustCreateProject(t, s, slug)
	planID, err := s.CreatePlan(ctx, CreatePlanOpts{
		ProjectID: projectID, Slug: slug, Title: slug,
		SourceMarkdown: "# P\n\n1. one\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	taskID = "bare"
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: taskID, Description: "bare"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return planID, taskID
}

// TestGetTaskExecutionMetadataMissingRowIsNamed makes the absent row
// self-describing. Wrapping sql.ErrNoRows generically forces every caller to
// either string-match or treat "no metadata" as an opaque failure, when it is
// the one condition the sentinel was introduced to name.
func TestGetTaskExecutionMetadataMissingRowIsNamed(t *testing.T) {
	s := openTestStore(t)
	planID, taskID := taskWithoutMetadata(t, s, "meta-get")

	_, err := s.GetTaskExecutionMetadata(context.Background(), planID, taskID)
	if !errors.Is(err, ErrTaskMetadataMissing) {
		t.Fatalf("err = %v, want ErrTaskMetadataMissing", err)
	}
}

// TestBindTaskCalibrationMissingRowIsNamed covers the diagnostic re-read: when
// the UPDATE affects no rows, the follow-up SELECT distinguishes "already bound
// to something else" from "there is nothing to bind". Only the first is a
// conflict.
func TestBindTaskCalibrationMissingRowIsNamed(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	planID, taskID := taskWithoutMetadata(t, s, "meta-bind")

	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO provider_calibrations(
			id, alias, provider, model, effort, binary_path, binary_version,
			binary_sha256, invocation_hash, inference_domain, control_domain,
			independence_domain, capabilities_json, evidence_json
		) VALUES ('calib-missing','a1','claude','opus','high','/bin/claude','1.0',
			'sha','hash','inf','ctl','ind','{}','{}')
	`); err != nil {
		t.Fatalf("seed calibration: %v", err)
	}

	err := s.BindTaskCalibration(ctx, planID, taskID, "calib-missing", `{"fanout":true}`)
	if !errors.Is(err, ErrTaskMetadataMissing) {
		t.Fatalf("err = %v, want ErrTaskMetadataMissing", err)
	}
}

// TestRecordTaskExecutionMissingRowIsNotAConflict is the sharpest of the three.
// The task IS legitimately claimed and running under the reporting session, so
// reporting ErrTaskExecutionConflict blames the caller for a provenance clash
// that never happened and hides the real cause — there is no metadata row to
// write into. A caller retrying a "conflict" would retry forever.
func TestRecordTaskExecutionMissingRowIsNotAConflict(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	planID, taskID := taskWithoutMetadata(t, s, "meta-exec")

	sessionID, workerID := mustCreateSessionAndWorker(t, s, "meta-exec")
	claimed, err := s.ClaimNextReady(ctx, planID, sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claimed.ID != taskID {
		t.Fatalf("claimed %q, want %q", claimed.ID, taskID)
	}

	err = s.RecordTaskExecution(ctx, planID, taskID,
		"alias", "claude", "opus", "high", "domain", sessionID)
	if errors.Is(err, ErrTaskExecutionConflict) {
		t.Fatalf("a task with no metadata row was reported as a provenance CONFLICT: %v", err)
	}
	if !errors.Is(err, ErrTaskMetadataMissing) {
		t.Fatalf("err = %v, want ErrTaskMetadataMissing", err)
	}
}
