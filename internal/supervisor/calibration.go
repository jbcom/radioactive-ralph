package supervisor

import (
	"context"
	"errors"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// The server routes calibration commands through a type assertion, so a
// Supervisor that stopped satisfying this interface would not fail to build —
// it would silently start answering unsupported_command, and the operator would
// see a refusal indistinguishable from an older supervisor. Assert it here so
// that regression is a compile error instead.
var _ ipc.CalibrationHandler = (*Supervisor)(nil)

// calibrationFromWire converts a wire record into the store shape.
//
// The ID is deliberately NOT carried across: the store content-addresses a
// calibration from the fields that identify the measurement, so an id from a
// client would either duplicate that derivation or assert an identity the
// supervisor cannot verify. Letting the store derive it is what keeps
// re-recording the same measurement idempotent.
func calibrationFromWire(record ipc.CalibrationRecord) store.ProviderCalibration {
	return store.ProviderCalibration{
		Alias: record.Alias, Provider: record.Provider,
		Model: record.Model, Effort: record.Effort,
		BinaryPath: record.BinaryPath, BinaryVersion: record.BinaryVersion,
		BinarySHA256: record.BinarySHA256, InvocationHash: record.InvocationHash,
		InferenceDomain: record.InferenceDomain, ControlDomain: record.ControlDomain,
		IndependenceDomain: record.IndependenceDomain, ModelDigest: record.ModelDigest,
		CapabilitiesJSON: record.CapabilitiesJSON, EvidenceJSON: record.EvidenceJSON,
	}
}

func calibrationToWire(value store.ProviderCalibration) ipc.CalibrationRecord {
	return ipc.CalibrationRecord{
		Alias: value.Alias, Provider: value.Provider,
		Model: value.Model, Effort: value.Effort,
		BinaryPath: value.BinaryPath, BinaryVersion: value.BinaryVersion,
		BinarySHA256: value.BinarySHA256, InvocationHash: value.InvocationHash,
		InferenceDomain: value.InferenceDomain, ControlDomain: value.ControlDomain,
		IndependenceDomain: value.IndependenceDomain, ModelDigest: value.ModelDigest,
		CapabilitiesJSON: value.CapabilitiesJSON, EvidenceJSON: value.EvidenceJSON,
	}
}

// HandleCalibrationPut records one provider calibration.
//
// This is the PRODUCER the independence domain needs. Dispatch already records
// what each task ran on, reading the domain from the binding's calibration —
// but with nothing able to record a calibration, that lookup always missed and
// every task's domain was empty. An empty domain reads as "independent", so a
// differentFrom constraint compared "" against "" and permitted everything.
//
// A conflict is surfaced as CodeConflict rather than overwriting. Silently
// replacing an alias's calibration would retroactively change what every task
// already recorded against that alias is believed to have run on — the audit
// trail would say tasks were independent on evidence that no longer exists.
func (s *Supervisor) HandleCalibrationPut(
	ctx context.Context,
	args ipc.CalibrationPutArgs,
) (ipc.CalibrationPutReply, error) {
	// Validate the record HERE rather than inferring "malformed" from whatever
	// the store returned. A locked or full database, a cancelled context, or any
	// other operational failure also comes back as a non-nil error, and labelling
	// those invalid_args tells the client its payload is permanently malformed —
	// so it declines to retry a request that would have succeeded, and reports
	// the wrong diagnosis. Coding only what this handler itself rejected keeps
	// storage failures as uncoded internal errors, matching the drive handlers.
	if args.Calibration.Alias == "" {
		return ipc.CalibrationPutReply{}, &codedError{ipc.CodeInvalidArgs, "calibration-put: alias required"}
	}
	if args.Calibration.Provider == "" {
		return ipc.CalibrationPutReply{}, &codedError{ipc.CodeInvalidArgs, "calibration-put: provider required"}
	}
	if args.Calibration.InvocationHash == "" {
		return ipc.CalibrationPutReply{}, &codedError{
			ipc.CodeInvalidArgs, "calibration-put: invocation hash required",
		}
	}

	recorded, err := s.store.RecordCalibration(ctx, calibrationFromWire(args.Calibration))
	switch {
	case errors.Is(err, store.ErrCalibrationConflict):
		return ipc.CalibrationPutReply{}, &codedError{ipc.CodeConflict, err.Error()}
	case err != nil:
		return ipc.CalibrationPutReply{}, fmt.Errorf("supervisor: record calibration: %w", err)
	}
	return ipc.CalibrationPutReply{ID: recorded.ID}, nil
}

// HandleCalibrationList enumerates the recorded calibrations so an operator can
// see which aliases have a measured independence domain and which do not —
// the difference between a differentFrom constraint that can be enforced and
// one that silently cannot.
func (s *Supervisor) HandleCalibrationList(ctx context.Context) (ipc.CalibrationListReply, error) {
	values, err := s.store.ListCalibrations(ctx)
	if err != nil {
		return ipc.CalibrationListReply{}, fmt.Errorf("supervisor: list calibrations: %w", err)
	}
	reply := ipc.CalibrationListReply{
		Calibrations: make([]ipc.CalibrationRecord, 0, len(values)),
	}
	for _, value := range values {
		reply.Calibrations = append(reply.Calibrations, calibrationToWire(value))
	}
	return reply, nil
}
