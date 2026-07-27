package store

import (
	"context"
	"testing"
)

// TestBindTaskCalibrationRejectsUnknownCalibration proves the referential
// constraint is live. BindTaskCalibration writes a caller-supplied id and does
// not itself check provider_calibrations, so without the FK an invalid or stale
// id would bind durably — and a bound calibration is meant to be the immutable
// record of how a task was executed.
func TestBindTaskCalibrationRejectsUnknownCalibration(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "calib-fk")
	planID := seedMetadataTask(t, s, projectID, "p-calib-fk", "t", "0.0", "team/a")

	err := s.BindTaskCalibration(ctx, planID, "t", "no-such-calibration", `{"caps":[]}`)
	if err == nil {
		t.Fatal("bound a calibration id that does not exist in provider_calibrations")
	}
}

// TestBindTaskCalibrationAcceptsRealCalibration is the other half: the
// constraint must not reject a legitimate bind.
func TestBindTaskCalibrationAcceptsRealCalibration(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "calib-ok")
	planID := seedMetadataTask(t, s, projectID, "p-calib-ok", "t", "0.0", "team/a")

	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO provider_calibrations(
			id, alias, provider, model, effort, binary_path, binary_version,
			binary_sha256, invocation_hash, inference_domain, control_domain,
			independence_domain, capabilities_json, evidence_json
		) VALUES ('calib-1','a1','claude','opus','high','/bin/claude','1.0',
			'sha','hash','inf','ctl','ind','{}','{}')
	`); err != nil {
		t.Fatalf("seed calibration: %v", err)
	}

	if err := s.BindTaskCalibration(ctx, planID, "t", "calib-1", `{"caps":[]}`); err != nil {
		t.Fatalf("BindTaskCalibration with a real id: %v", err)
	}
	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "t")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if meta.CalibrationID != "calib-1" {
		t.Fatalf("CalibrationID = %q, want calib-1", meta.CalibrationID)
	}
}

// TestMarkBlockedTransitionsBetweenBlockCauses covers the allow-list gap: a task
// already blocked on inputs must be re-markable when a capability block applies
// (and vice versa). Omitting blocked_input silently matched zero rows, which the
// RowsAffected check then turned into a hard failure.
func TestMarkBlockedTransitionsBetweenBlockCauses(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "block-transitions")

	t.Run("input then capability", func(t *testing.T) {
		planID := seedMetadataTask(t, s, projectID, "p-in-cap", "t", "0.0", "team/a")
		if err := s.MarkBlockedInput(ctx, planID, "t", "input missing"); err != nil {
			t.Fatalf("MarkBlockedInput: %v", err)
		}
		if err := s.MarkBlockedCapability(ctx, planID, "t", "capability missing"); err != nil {
			t.Fatalf("MarkBlockedCapability after an input block: %v", err)
		}
		meta, err := s.GetTaskExecutionMetadata(ctx, planID, "t")
		if err != nil {
			t.Fatalf("GetTaskExecutionMetadata: %v", err)
		}
		if meta.BlockedReason != "capability missing" {
			t.Errorf("BlockedReason = %q, want the newer cause", meta.BlockedReason)
		}
	})

	t.Run("input then input again", func(t *testing.T) {
		planID := seedMetadataTask(t, s, projectID, "p-in-in", "t", "0.0", "team/a")
		if err := s.MarkBlockedInput(ctx, planID, "t", "first reason"); err != nil {
			t.Fatalf("first MarkBlockedInput: %v", err)
		}
		if err := s.MarkBlockedInput(ctx, planID, "t", "second reason"); err != nil {
			t.Fatalf("re-marking the same block cause: %v", err)
		}
		meta, err := s.GetTaskExecutionMetadata(ctx, planID, "t")
		if err != nil {
			t.Fatalf("GetTaskExecutionMetadata: %v", err)
		}
		if meta.BlockedReason != "second reason" {
			t.Errorf("BlockedReason = %q, want the updated reason", meta.BlockedReason)
		}
	})
}
