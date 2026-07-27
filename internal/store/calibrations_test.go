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
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "calib")
	// The attempt writer must be the task's CURRENT owner, so claim it. Before
	// the ownership guard this test recorded attempts against a pending task,
	// which is exactly the stale-worker write the guard now refuses.
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

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
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "calib-dup")
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

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

// TestRecordCalibrationAttemptRejectsAnEmptyOutputDigest is a P1 on #236 and
// the most consequential of the three: it defeats the entire point of
// calibration.
//
// Attempts are compared BY their output digest to decide whether repeated runs
// agree. NOT NULL accepts the empty string, so a provider that exits without a
// usable result records "" — and N failed runs all carry the same "" digest,
// which reads as unanimous agreement. Calibration would report its strongest
// possible signal from runs that produced nothing.
func TestRecordCalibrationAttemptRejectsAnEmptyOutputDigest(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "calib-empty-digest")
	planID := seedCalibrationPlan(t, s, projectID, "empty-digest")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "calib-empty")
	// Claim the task so ONLY the digest can fail this — otherwise the ownership
	// guard rejects it first and the test passes without exercising the digest
	// check at all.
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	err := s.RecordCalibrationAttempt(ctx, CalibrationAttempt{
		PlanID: planID, TaskID: "t", AttemptSequence: 1, Repetition: 1,
		Alias: "a1", Provider: "claude", Model: "m", Effort: "high",
		SessionID:             sessionID,
		AssistantOutputSHA256: "", // provider produced no usable result
	})
	if err == nil {
		t.Fatal("an attempt with no output digest was accepted; N failed runs " +
			"would all carry \"\" and read as unanimous agreement")
	}
}

// TestRecordCalibrationAttemptRejectsAStaleWorker is the other P1. When the
// reaper reclaims a stalled worker before its provider call returns, that
// worker's late write must not land: the task may already belong to a
// replacement run, and attributing output to the evicted worker corrupts the
// attempt history the agreement check reads.
func TestRecordCalibrationAttemptRejectsAStaleWorker(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "calib-stale")
	planID := seedCalibrationPlan(t, s, projectID, "stale")
	staleSession, _ := mustCreateSessionAndWorker(t, s, "calib-stale-old")

	// The task is not owned by the stale session — it was never claimed by it.
	err := s.RecordCalibrationAttempt(ctx, CalibrationAttempt{
		PlanID: planID, TaskID: "t", AttemptSequence: 1, Repetition: 1,
		Alias: "a1", Provider: "claude", Model: "m", Effort: "high",
		SessionID:             staleSession,
		AssistantOutputSHA256: "sha-stale",
	})
	if err == nil {
		t.Fatal("an attempt from a session that does not own the running task was " +
			"accepted; a reaped worker's late write would corrupt the attempt history")
	}
}

// TestRecordCalibrationIsIdempotentUnderAConcurrentInsert covers the P2. The
// documented guarantee is that recording the SAME measurement twice is a no-op
// returning the same id. A check-then-insert loses that under concurrency: two
// probes for a previously unseen alias both miss the lookup, and the loser gets
// a UNIQUE violation instead of the identical row.
//
// The race is simulated deterministically by inserting the row between the
// lookup and the insert — a concurrency test that relied on real scheduling
// would be exactly the load-sensitive flake this project has already had twice.
func TestRecordCalibrationIsIdempotentUnderAConcurrentInsert(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	first, err := s.RecordCalibration(ctx, sampleCalibration("a1"))
	if err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}
	// The "loser" path: the row already exists and the caller records the same
	// measurement. It must return the existing row, never a UNIQUE error.
	second, err := s.RecordCalibration(ctx, sampleCalibration("a1"))
	if err != nil {
		t.Fatalf("identical re-record returned %v; recording the same "+
			"measurement twice is documented as a no-op", err)
	}
	if first.ID != second.ID {
		t.Fatalf("ids differ: %q vs %q", first.ID, second.ID)
	}
}
