package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func calibrationFromWire(record ipc.CalibrationRecord) store.ProviderCalibration {
	return store.ProviderCalibration{
		ID: record.ID, Alias: record.Alias, Provider: record.Provider,
		Model: record.Model, Effort: record.Effort,
		BinaryPath: record.BinaryPath, BinaryVersion: record.BinaryVersion,
		BinarySHA256: record.BinarySHA256, InvocationHash: record.InvocationHash,
		InferenceDomain: record.InferenceDomain, ControlDomain: record.ControlDomain,
		IndependenceDomain: record.IndependenceDomain, ModelDigest: record.ModelDigest,
		Capabilities: record.Capabilities, EvidenceJSON: string(record.Evidence),
	}
}

func calibrationToWire(value store.ProviderCalibration) ipc.CalibrationRecord {
	return ipc.CalibrationRecord{
		ID: value.ID, Alias: value.Alias, Provider: value.Provider,
		Model: value.Model, Effort: value.Effort,
		BinaryPath: value.BinaryPath, BinaryVersion: value.BinaryVersion,
		BinarySHA256: value.BinarySHA256, InvocationHash: value.InvocationHash,
		InferenceDomain: value.InferenceDomain, ControlDomain: value.ControlDomain,
		IndependenceDomain: value.IndependenceDomain, ModelDigest: value.ModelDigest,
		Capabilities: value.Capabilities, Evidence: []byte(value.EvidenceJSON),
	}
}

// HandleCalibrationPut imports evidence through the OS-authenticated local
// control endpoint after proving it still matches this host.
func (s *Supervisor) HandleCalibrationPut(
	ctx context.Context,
	args ipc.CalibrationPutArgs,
) (ipc.CalibrationPutReply, error) {
	value := calibrationFromWire(args.Calibration)
	if _, err := orch.ValidateProviderCalibration(value); err != nil {
		return ipc.CalibrationPutReply{}, &codedError{
			ipc.CodeInvalidArgs, fmt.Sprintf("calibration-put: %v", err),
		}
	}
	id, err := s.store.PutProviderCalibration(ctx, value)
	if err != nil {
		return ipc.CalibrationPutReply{}, &codedError{ipc.CodeInvalidArgs, err.Error()}
	}
	return ipc.CalibrationPutReply{ID: id}, nil
}

// HandleCalibrationGet loads one immutable calibration for the local API.
func (s *Supervisor) HandleCalibrationGet(
	ctx context.Context,
	args ipc.CalibrationGetArgs,
) (ipc.CalibrationRecord, error) {
	if args.ID == "" {
		return ipc.CalibrationRecord{}, &codedError{ipc.CodeInvalidArgs, "calibration-get: id required"}
	}
	value, err := s.store.GetProviderCalibration(ctx, args.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ipc.CalibrationRecord{}, &codedError{ipc.CodeNotFound, err.Error()}
	}
	if err != nil {
		return ipc.CalibrationRecord{}, fmt.Errorf("supervisor: get calibration: %w", err)
	}
	return calibrationToWire(value), nil
}

// HandleCalibrationList returns every immutable calibration in alias order.
func (s *Supervisor) HandleCalibrationList(ctx context.Context) (ipc.CalibrationListReply, error) {
	values, err := s.store.ListProviderCalibrations(ctx)
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
