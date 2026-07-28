package supervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jonboulle/clockwork"
)

func sampleCalibration() ipc.CalibrationRecord {
	return ipc.CalibrationRecord{
		Alias: "claude", Provider: "claude", Model: "sonnet", Effort: "medium",
		BinaryPath: "/usr/bin/claude", BinaryVersion: "1.0", BinarySHA256: "abc",
		InvocationHash:  "hash-1",
		InferenceDomain: "anthropic", ControlDomain: "anthropic",
		IndependenceDomain: "anthropic",
	}
}

// TestCalibrationPutMakesTheIndependenceDomainReadable is the point of the
// whole surface.
//
// Dispatch records what each task ran on and reads the independence domain from
// the binding's calibration — but with nothing able to RECORD one, that lookup
// always missed and every task's domain was empty. An empty domain reads as
// "independent", so a differentFrom constraint compared "" against "" and
// permitted everything. This asserts the recorded domain is actually readable
// back by the alias dispatch looks up.
func TestCalibrationPutMakesTheIndependenceDomainReadable(t *testing.T) {
	ctx := context.Background()
	s := newTestSupervisor(t, clockwork.NewFakeClock())

	reply, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{
		Calibration: sampleCalibration(),
	})
	if err != nil {
		t.Fatalf("HandleCalibrationPut: %v", err)
	}
	if reply.ID == "" {
		t.Fatal("reply carries no id — the store content-addresses a calibration, " +
			"so an empty id means nothing was recorded")
	}

	// The alias is what dispatch resolves against.
	got, err := s.store.GetCalibrationByAlias(ctx, "claude")
	if err != nil {
		t.Fatalf("GetCalibrationByAlias after a put: %v", err)
	}
	if got.IndependenceDomain != "anthropic" {
		t.Fatalf("independence domain = %q, want %q — an empty domain here is "+
			"exactly what makes differentFrom compare \"\" against \"\" and permit "+
			"everything", got.IndependenceDomain, "anthropic")
	}
	if got.ID != reply.ID {
		t.Errorf("stored id %q != returned id %q", got.ID, reply.ID)
	}
}

// TestCalibrationPutIsIdempotentForTheSameMeasurement covers re-probing. The id
// is content-addressed, so recording the same measurement twice must hit the
// existing row rather than conflict or accumulate.
func TestCalibrationPutIsIdempotentForTheSameMeasurement(t *testing.T) {
	ctx := context.Background()
	s := newTestSupervisor(t, clockwork.NewFakeClock())

	first, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: sampleCalibration()})
	if err != nil {
		t.Fatalf("first put: %v", err)
	}
	second, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: sampleCalibration()})
	if err != nil {
		t.Fatalf("second put of the SAME measurement must be idempotent, got: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("ids differ across identical measurements (%q vs %q) — re-probing "+
			"would accumulate a row per probe", first.ID, second.ID)
	}

	list, err := s.HandleCalibrationList(ctx)
	if err != nil {
		t.Fatalf("HandleCalibrationList: %v", err)
	}
	if len(list.Calibrations) != 1 {
		t.Fatalf("list holds %d calibrations, want 1", len(list.Calibrations))
	}
}

// TestCalibrationPutRefusesToReplaceALiveAlias pins the conflict as a REFUSAL
// rather than an overwrite, and as CodeConflict rather than a generic error.
//
// Silently replacing an alias's calibration would retroactively change what
// every task already recorded against that alias is believed to have run on:
// the audit trail would assert tasks were independent on the strength of
// evidence that no longer exists.
func TestCalibrationPutRefusesToReplaceALiveAlias(t *testing.T) {
	ctx := context.Background()
	s := newTestSupervisor(t, clockwork.NewFakeClock())

	if _, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{
		Calibration: sampleCalibration(),
	}); err != nil {
		t.Fatalf("first put: %v", err)
	}

	// Same alias, DIFFERENT measurement.
	other := sampleCalibration()
	other.InvocationHash = "hash-2"
	other.IndependenceDomain = "somewhere-else"
	_, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: other})
	if err == nil {
		t.Fatal("re-recording a DIFFERENT measurement under a live alias was accepted — " +
			"that silently rewrites what already-dispatched tasks are believed to have run on")
	}
	var coded *codedError
	if !errors.As(err, &coded) || coded.Code() != ipc.CodeConflict {
		t.Fatalf("err = %v, want a CodeConflict codedError so a client can react "+
			"programmatically instead of string-matching", err)
	}

	// And the original survives unchanged.
	got, err := s.store.GetCalibrationByAlias(ctx, "claude")
	if err != nil {
		t.Fatalf("GetCalibrationByAlias: %v", err)
	}
	if got.IndependenceDomain != "anthropic" {
		t.Errorf("domain = %q after a refused replace, want the original %q",
			got.IndependenceDomain, "anthropic")
	}
}

// TestCalibrationPutRejectsAnIncompleteRecord covers the validation the store
// already enforces, through the wire path: a record without the fields that
// identify a measurement cannot be content-addressed, so accepting it would
// store something no lookup could ever match.
func TestCalibrationPutRejectsAnIncompleteRecord(t *testing.T) {
	ctx := context.Background()
	s := newTestSupervisor(t, clockwork.NewFakeClock())

	for _, tc := range []struct {
		name  string
		mutar func(*ipc.CalibrationRecord)
	}{
		{"no alias", func(c *ipc.CalibrationRecord) { c.Alias = "" }},
		{"no provider", func(c *ipc.CalibrationRecord) { c.Provider = "" }},
		{"no invocation hash", func(c *ipc.CalibrationRecord) { c.InvocationHash = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := sampleCalibration()
			tc.mutar(&rec)
			if _, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{
				Calibration: rec,
			}); err == nil {
				t.Fatal("incomplete calibration accepted — it cannot be content-addressed, " +
					"so no dispatch lookup could ever match it")
			}
		})
	}
}

// TestCalibrationPutCodesOnlyItsOwnValidation is the fix for a review finding.
//
// A catch-all that coded every store error as invalid_args told the client its
// payload was permanently malformed whenever SQLite was locked or full or the
// context was cancelled — so a client would decline to retry a request that
// would have succeeded, and would report the wrong diagnosis. Argument
// validation is coded; storage failures stay uncoded internal errors, matching
// the drive handlers.
func TestCalibrationPutCodesOnlyItsOwnValidation(t *testing.T) {
	ctx := context.Background()
	s := newTestSupervisor(t, clockwork.NewFakeClock())

	// A malformed record IS the handler's own rejection: coded.
	rec := sampleCalibration()
	rec.Alias = ""
	_, err := s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: rec})
	var coded *codedError
	if !errors.As(err, &coded) || coded.Code() != ipc.CodeInvalidArgs {
		t.Fatalf("err = %v, want CodeInvalidArgs for a record the handler itself rejects", err)
	}

	// A closed store is an OPERATIONAL failure. It must NOT be coded
	// invalid_args: the payload is fine and a retry could succeed.
	if err := s.store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	_, err = s.HandleCalibrationPut(ctx, ipc.CalibrationPutArgs{Calibration: sampleCalibration()})
	if err == nil {
		t.Fatal("a put against a closed store reported success")
	}
	if errors.As(err, &coded) && coded.Code() == ipc.CodeInvalidArgs {
		t.Fatalf("storage failure coded as invalid_args (%v) — a client would treat a "+
			"retryable operational error as a permanently malformed payload", err)
	}
}
