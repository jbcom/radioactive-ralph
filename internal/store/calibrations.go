package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrCalibrationNotFound reports an alias with no recorded measurement.
//
// Distinct from a zero-value calibration, which a caller could mistake for a
// real measurement of an empty command line.
var ErrCalibrationNotFound = errors.New("store: no calibration recorded for this alias")

// ErrCalibrationConflict reports the same alias measured against a DIFFERENT
// command line than the one already recorded.
var ErrCalibrationConflict = errors.New("store: alias already calibrated against a different invocation")

// ErrCalibrationAttemptNoOutput reports an attempt with no output digest.
//
// Attempts are identified by what they produced, and agreement is computed by
// comparing digests — so an empty one is not a degraded record but an actively
// misleading one: repeated failures would all match.
var ErrCalibrationAttemptNoOutput = errors.New("store: calibration attempt has no output digest")

// ProviderCalibration is one measurement of one exact provider command line.
//
// It is a RECORD OF AN OBSERVATION, not configuration. That is why it is
// content-addressed and why a conflicting remeasurement fails rather than
// overwrites: tasks bind to a calibration id to document what they ran on, and
// silently changing the row underneath them would retroactively rewrite that
// history.
type ProviderCalibration struct {
	ID       string
	Alias    string
	Provider string
	Model    string
	Effort   string

	BinaryPath    string
	BinaryVersion string
	BinarySHA256  string
	// InvocationHash fingerprints the whole binding config plus the exact
	// model/effort (see provider.InvocationConfigHash). Two calibrations with
	// different hashes measured different command lines, whatever else matches.
	InvocationHash string

	// The three domains an independence constraint is evaluated against: who
	// runs the inference, who controls the endpoint, and who the result is
	// independent OF. Distinct because they can differ — a self-hosted model
	// behind a vendor's control plane shares one domain and not the others.
	InferenceDomain    string
	ControlDomain      string
	IndependenceDomain string

	ModelDigest      string
	CapabilitiesJSON string
	EvidenceJSON     string
}

// contentAddress derives the calibration id from everything that identifies the
// measurement.
//
// Content addressing is what makes re-probing idempotent: the same command line
// measured twice yields the same id, so a lookup hits instead of accumulating a
// new row per probe. Capabilities and evidence are deliberately EXCLUDED — they
// are what was observed, not what was observed ABOUT, and including them would
// mint a new id every time a probe's evidence differed by a timestamp.
func (c ProviderCalibration) contentAddress() (string, error) {
	raw, err := json.Marshal(struct {
		Alias              string `json:"alias"`
		Provider           string `json:"provider"`
		Model              string `json:"model"`
		Effort             string `json:"effort"`
		BinaryPath         string `json:"binary_path"`
		BinaryVersion      string `json:"binary_version"`
		BinarySHA256       string `json:"binary_sha256"`
		InvocationHash     string `json:"invocation_hash"`
		InferenceDomain    string `json:"inference_domain"`
		ControlDomain      string `json:"control_domain"`
		IndependenceDomain string `json:"independence_domain"`
		ModelDigest        string `json:"model_digest"`
	}{
		Alias: c.Alias, Provider: c.Provider, Model: c.Model, Effort: c.Effort,
		BinaryPath: c.BinaryPath, BinaryVersion: c.BinaryVersion,
		BinarySHA256: c.BinarySHA256, InvocationHash: c.InvocationHash,
		InferenceDomain: c.InferenceDomain, ControlDomain: c.ControlDomain,
		IndependenceDomain: c.IndependenceDomain, ModelDigest: c.ModelDigest,
	})
	if err != nil {
		return "", fmt.Errorf("store: marshal calibration identity: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// RecordCalibration stores a measurement, returning it with its content
// address filled in.
//
// Recording the same measurement twice is a no-op that returns the same id.
// Recording a DIFFERENT measurement under an alias that already has one fails
// with ErrCalibrationConflict rather than overwriting — an upgraded binary or a
// changed invocation is a new fact, and quietly replacing the old one would
// change what every already-bound task is documented to have run on.
func (s *Store) RecordCalibration(ctx context.Context, c ProviderCalibration) (ProviderCalibration, error) {
	if c.Alias == "" || c.Provider == "" || c.InvocationHash == "" {
		return ProviderCalibration{}, fmt.Errorf("store: calibration requires alias, provider, and invocation hash")
	}
	id, err := c.contentAddress()
	if err != nil {
		return ProviderCalibration{}, err
	}
	c.ID = id

	existing, err := s.GetCalibrationByAlias(ctx, c.Alias)
	switch {
	case err == nil && existing.ID == id:
		// Same measurement, already recorded.
		return existing, nil
	case err == nil:
		return ProviderCalibration{}, fmt.Errorf(
			"%w: alias %q is calibrated as %s, cannot re-record as %s",
			ErrCalibrationConflict, c.Alias, existing.ID[:12], id[:12])
	case !errors.Is(err, ErrCalibrationNotFound):
		return ProviderCalibration{}, err
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_calibrations(
			id, alias, provider, model, effort, binary_path, binary_version,
			binary_sha256, invocation_hash, inference_domain, control_domain,
			independence_domain, model_digest, capabilities_json, evidence_json
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		c.ID, c.Alias, c.Provider, c.Model, c.Effort, c.BinaryPath, c.BinaryVersion,
		c.BinarySHA256, c.InvocationHash, c.InferenceDomain, c.ControlDomain,
		c.IndependenceDomain, nullIfEmpty(c.ModelDigest),
		c.CapabilitiesJSON, c.EvidenceJSON,
	); err != nil {
		// Lost the insert race. The check-then-insert above is not atomic, so a
		// concurrent probe for a previously unseen alias can land between them —
		// and the documented guarantee is that recording the SAME measurement
		// twice is a no-op, not a UNIQUE error. Re-read and compare: identical
		// means the other writer already recorded exactly this, and a genuine
		// disagreement is still a conflict.
		if isUniqueViolation(err) {
			existing, getErr := s.GetCalibrationByAlias(ctx, c.Alias)
			if getErr != nil {
				return ProviderCalibration{}, fmt.Errorf(
					"store: record calibration %q raced and could not be reloaded: %w", c.Alias, getErr)
			}
			if existing.ID == id {
				return existing, nil
			}
			return ProviderCalibration{}, fmt.Errorf(
				"%w: alias %q is calibrated as %s, cannot re-record as %s",
				ErrCalibrationConflict, c.Alias, existing.ID[:12], id[:12])
		}
		return ProviderCalibration{}, fmt.Errorf("store: record calibration %q: %w", c.Alias, err)
	}
	return c, nil
}

// GetCalibrationByAlias returns the measurement recorded for alias, or
// ErrCalibrationNotFound.
func (s *Store) GetCalibrationByAlias(ctx context.Context, alias string) (ProviderCalibration, error) {
	var c ProviderCalibration
	var modelDigest sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, alias, provider, model, effort, binary_path, binary_version,
		       binary_sha256, invocation_hash, inference_domain, control_domain,
		       independence_domain, model_digest, capabilities_json, evidence_json
		FROM provider_calibrations WHERE alias = ?
	`, alias).Scan(
		&c.ID, &c.Alias, &c.Provider, &c.Model, &c.Effort, &c.BinaryPath,
		&c.BinaryVersion, &c.BinarySHA256, &c.InvocationHash, &c.InferenceDomain,
		&c.ControlDomain, &c.IndependenceDomain, &modelDigest,
		&c.CapabilitiesJSON, &c.EvidenceJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProviderCalibration{}, fmt.Errorf("%w: %s", ErrCalibrationNotFound, alias)
		}
		return ProviderCalibration{}, fmt.Errorf("store: get calibration %q: %w", alias, err)
	}
	c.ModelDigest = modelDigest.String
	return c, nil
}

// CalibrationAttempt is one repetition of one calibrated task run.
//
// Calibration compares repeated runs of the SAME task, so each repetition is
// recorded separately and identified by the output it produced — agreement
// across repetitions is the signal, and that cannot be computed from a single
// collapsed row.
type CalibrationAttempt struct {
	PlanID          string
	TaskID          string
	AttemptSequence int
	Repetition      int

	Alias    string
	Provider string
	Model    string
	Effort   string

	SessionID             string
	ProviderSessionID     string
	AssistantOutputSHA256 string
}

// RecordCalibrationAttempt stores one repetition.
//
// The primary key covers (plan, task, attempt, repetition), so a double-record
// fails rather than silently double-counting one result when runs are compared
// for agreement.
func (s *Store) RecordCalibrationAttempt(ctx context.Context, a CalibrationAttempt) error {
	if a.PlanID == "" || a.TaskID == "" {
		return fmt.Errorf("store: calibration attempt requires plan and task")
	}
	if a.AttemptSequence <= 0 || a.Repetition <= 0 {
		return fmt.Errorf("store: calibration attempt sequence and repetition must be positive")
	}
	// An attempt is IDENTIFIED by the output it produced, and agreement across
	// repetitions is computed by comparing those digests. NOT NULL accepts the
	// empty string, so a provider that exited without a usable result would
	// record "" — and N such runs would all match each other, reading as
	// unanimous agreement from runs that produced nothing. Reject it here rather
	// than teach every comparison to special-case a sentinel.
	if a.AssistantOutputSHA256 == "" {
		return fmt.Errorf("%w: attempt %s/%s rep %d has no output digest",
			ErrCalibrationAttemptNoOutput, a.PlanID, a.TaskID, a.Repetition)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin record calibration attempt: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The write must belong to the worker that CURRENTLY owns the task. A worker
	// the reaper reclaimed mid-turn still returns from its provider call and
	// still tries to record; by then the task may belong to a replacement run,
	// and accepting the late write would attribute output to an evicted worker
	// and corrupt the very history the agreement check reads. Checked inside the
	// transaction so ownership cannot change between the check and the insert.
	var status, owner string
	if err := tx.QueryRowContext(ctx, `
		SELECT status, COALESCE(claimed_by_session,'') FROM tasks WHERE plan_id = ? AND id = ?
	`, a.PlanID, a.TaskID).Scan(&status, &owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no task %s/%s", ErrTaskNotOwnedRunning, a.PlanID, a.TaskID)
		}
		return fmt.Errorf("store: read calibration attempt task owner: %w", err)
	}
	if status != string(TaskStatusRunning) || owner != a.SessionID {
		return fmt.Errorf(
			"%w: task %s/%s is %s owned by %q, attempt claims %q",
			ErrTaskNotOwnedRunning, a.PlanID, a.TaskID, status, owner, a.SessionID)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_calibration_attempts(
			plan_id, task_id, attempt_sequence, repetition, alias, provider,
			model, effort, session_id, provider_session_id, assistant_output_sha256
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)
	`,
		a.PlanID, a.TaskID, a.AttemptSequence, a.Repetition, a.Alias, a.Provider,
		a.Model, a.Effort, a.SessionID, nullIfEmpty(a.ProviderSessionID),
		a.AssistantOutputSHA256,
	); err != nil {
		return fmt.Errorf("store: record calibration attempt %s/%s rep %d: %w",
			a.PlanID, a.TaskID, a.Repetition, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit calibration attempt: %w", err)
	}
	return nil
}

// ListCalibrationAttempts returns one task's attempts in RUN ORDER, so a caller
// comparing outputs walks them the way they happened rather than however the
// storage engine returns them.
func (s *Store) ListCalibrationAttempts(ctx context.Context, planID, taskID string) ([]CalibrationAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plan_id, task_id, attempt_sequence, repetition, alias, provider,
		       model, effort, session_id, COALESCE(provider_session_id,''),
		       assistant_output_sha256
		FROM task_calibration_attempts
		WHERE plan_id = ? AND task_id = ?
		ORDER BY attempt_sequence, repetition
	`, planID, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list calibration attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CalibrationAttempt
	for rows.Next() {
		var a CalibrationAttempt
		if err := rows.Scan(
			&a.PlanID, &a.TaskID, &a.AttemptSequence, &a.Repetition, &a.Alias,
			&a.Provider, &a.Model, &a.Effort, &a.SessionID, &a.ProviderSessionID,
			&a.AssistantOutputSHA256,
		); err != nil {
			return nil, fmt.Errorf("store: scan calibration attempt: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate calibration attempts: %w", err)
	}
	return out, nil
}
