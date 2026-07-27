package store

import (
	"context"
	"errors"
	"testing"
)

func sampleCalibration(alias string) ProviderCalibration {
	return ProviderCalibration{
		Alias:              alias,
		Provider:           "claude",
		Model:              "claude-opus-5",
		Effort:             "high",
		BinaryPath:         "/usr/local/bin/claude",
		BinaryVersion:      "2.1.220",
		BinarySHA256:       "abc123",
		InvocationHash:     "hash-1",
		InferenceDomain:    "anthropic",
		ControlDomain:      "anthropic",
		IndependenceDomain: "anthropic",
		CapabilitiesJSON:   `{"fanout":true}`,
		EvidenceJSON:       `{"probed_at":"2026-07-27"}`,
	}
}

// TestRecordCalibrationIsContentAddressed is the property the whole feature
// rests on. A calibration is a MEASUREMENT of one exact command line, so
// recording the same measurement twice must yield the same id — otherwise every
// probe creates a new row and nothing can be looked up.
func TestRecordCalibrationIsContentAddressed(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	first, err := s.RecordCalibration(ctx, sampleCalibration("a1"))
	if err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}
	second, err := s.RecordCalibration(ctx, sampleCalibration("a1"))
	if err != nil {
		t.Fatalf("RecordCalibration again: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("ids differ for identical calibrations: %q vs %q — every probe "+
			"would create a new row and no lookup could ever hit", first.ID, second.ID)
	}
}

// TestRecordCalibrationRefusesAConflictingRemeasurement fails closed on the
// case that actually matters: the SAME alias measured against a DIFFERENT
// command line. Silently overwriting would retroactively change what every task
// already bound to that alias is documented to have run on.
func TestRecordCalibrationRefusesAConflictingRemeasurement(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	if _, err := s.RecordCalibration(ctx, sampleCalibration("a1")); err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}

	changed := sampleCalibration("a1")
	changed.BinaryVersion = "2.2.0" // the binary was upgraded under us
	changed.InvocationHash = "hash-2"
	_, err := s.RecordCalibration(ctx, changed)
	if !errors.Is(err, ErrCalibrationConflict) {
		t.Fatalf("err = %v, want ErrCalibrationConflict — silently overwriting would "+
			"retroactively change what already-bound tasks ran on", err)
	}
}

// TestGetCalibrationByAliasFindsTheCurrentMeasurement covers the lookup
// dispatch needs: resolve an alias to the calibration a task may bind to.
func TestGetCalibrationByAliasFindsTheCurrentMeasurement(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	want, err := s.RecordCalibration(ctx, sampleCalibration("a1"))
	if err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}
	got, err := s.GetCalibrationByAlias(ctx, "a1")
	if err != nil {
		t.Fatalf("GetCalibrationByAlias: %v", err)
	}
	if got.ID != want.ID || got.Model != "claude-opus-5" || got.Effort != "high" {
		t.Fatalf("got %+v, want the recorded calibration", got)
	}
}

// TestGetCalibrationByAliasIsNotFoundForAnUnknownAlias fails closed rather than
// returning a zero calibration, which a caller could mistake for a real
// measurement of an empty command line.
func TestGetCalibrationByAliasIsNotFoundForAnUnknownAlias(t *testing.T) {
	s := openTestStore(t)
	_, err := s.GetCalibrationByAlias(context.Background(), "never-probed")
	if !errors.Is(err, ErrCalibrationNotFound) {
		t.Fatalf("err = %v, want ErrCalibrationNotFound", err)
	}
}

// TestRecordCalibrationAttemptTracksRepetitions covers the attempts table.
// Calibration compares repeated runs of the same task, so each repetition is
// recorded separately and identified by the OUTPUT it produced.
func TestRecordCalibrationAttemptTracksRepetitions(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "calib-attempts")
	planID := seedCalibrationPlan(t, s, projectID, "attempts")
	sessionID, _ := mustCreateSessionAndWorker(t, s, "calib")

	for rep := 1; rep <= 3; rep++ {
		if err := s.RecordCalibrationAttempt(ctx, CalibrationAttempt{
			PlanID: planID, TaskID: "t",
			AttemptSequence: 1, Repetition: rep,
			Alias: "a1", Provider: "claude", Model: "claude-opus-5", Effort: "high",
			SessionID:             sessionID,
			AssistantOutputSHA256: "sha-" + string(rune('a'+rep)),
		}); err != nil {
			t.Fatalf("RecordCalibrationAttempt rep %d: %v", rep, err)
		}
	}

	attempts, err := s.ListCalibrationAttempts(ctx, planID, "t")
	if err != nil {
		t.Fatalf("ListCalibrationAttempts: %v", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("got %d attempts, want 3", len(attempts))
	}
	// Ordered by repetition so a caller comparing outputs walks them in the
	// order they ran rather than whatever the storage engine returns.
	for i, a := range attempts {
		if a.Repetition != i+1 {
			t.Fatalf("attempt %d has repetition %d; results must come back in run order", i, a.Repetition)
		}
	}
}

// TestRecordCalibrationAttemptRejectsADuplicateRepetition fails closed on a
// double-record. Two rows for one repetition would silently double-count a
// result when comparing runs for agreement.
func TestRecordCalibrationAttemptRejectsADuplicateRepetition(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "calib-dup")
	planID := seedCalibrationPlan(t, s, projectID, "dup")
	sessionID, _ := mustCreateSessionAndWorker(t, s, "calib-dup")

	attempt := CalibrationAttempt{
		PlanID: planID, TaskID: "t", AttemptSequence: 1, Repetition: 1,
		Alias: "a1", Provider: "claude", Model: "m", Effort: "high",
		SessionID: sessionID, AssistantOutputSHA256: "sha",
	}
	if err := s.RecordCalibrationAttempt(ctx, attempt); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := s.RecordCalibrationAttempt(ctx, attempt); err == nil {
		t.Fatal("a duplicate repetition was accepted; comparing runs would " +
			"double-count one result")
	}
}

// seedCalibrationPlan creates a one-task plan so attempts have a real task to
// reference — task_calibration_attempts has a foreign key into tasks.
func seedCalibrationPlan(t *testing.T, s *Store, projectID, slug string) string {
	t.Helper()
	planID, err := s.CreatePlanGraph(context.Background(), CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: slug, Title: slug},
		Tasks:          []GraphTaskSpec{graphTask("t", "the task", "0")},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}
	return planID
}
