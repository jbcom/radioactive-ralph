package main

import (
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func calibrationRecordToStore(record ipc.CalibrationRecord) store.ProviderCalibration {
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

func calibrationStoreToRecord(value store.ProviderCalibration) ipc.CalibrationRecord {
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
