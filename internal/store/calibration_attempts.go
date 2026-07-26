package store

import (
	"context"
	"fmt"
)

// CalibrationAttempt is durable provenance for one independent repetition of
// a calibration fixture task.
type CalibrationAttempt struct {
	PlanID                string
	TaskID                string
	AttemptSequence       int
	Repetition            int
	Alias                 string
	Provider              string
	Model                 string
	Effort                string
	SessionID             string
	ProviderSessionID     string
	AssistantOutputSHA256 string
}

// RecordCalibrationAttempt writes one repetition exactly once. The compound
// primary key prevents retries or racing dispatchers from silently replacing
// evidence already used by an adjudicator.
func (s *Store) RecordCalibrationAttempt(ctx context.Context, attempt CalibrationAttempt) error {
	if attempt.PlanID == "" || attempt.TaskID == "" || attempt.AttemptSequence < 1 ||
		attempt.Repetition < 1 ||
		attempt.Alias == "" || attempt.Provider == "" || attempt.Model == "" ||
		attempt.Effort == "" || attempt.SessionID == "" ||
		len(attempt.AssistantOutputSHA256) != 64 {
		return fmt.Errorf("store: complete calibration attempt provenance required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_calibration_attempts(
			plan_id, task_id, attempt_sequence, repetition, alias, provider, model, effort,
			session_id, provider_session_id, assistant_output_sha256
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, attempt.PlanID, attempt.TaskID, attempt.AttemptSequence, attempt.Repetition,
		attempt.Alias, attempt.Provider, attempt.Model, attempt.Effort,
		attempt.SessionID, nullIfEmpty(attempt.ProviderSessionID),
		attempt.AssistantOutputSHA256)
	if err != nil {
		return fmt.Errorf("store: record calibration attempt: %w", err)
	}
	return nil
}

// ListCalibrationAttempts returns a fixture task's repetitions in execution
// order for adjudication and audit.
func (s *Store) ListCalibrationAttempts(
	ctx context.Context,
	planID, taskID string,
) ([]CalibrationAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plan_id, task_id, attempt_sequence, repetition, alias, provider, model, effort,
		       session_id, COALESCE(provider_session_id,''),
		       assistant_output_sha256
		FROM task_calibration_attempts
		WHERE plan_id = ? AND task_id = ?
		ORDER BY attempt_sequence, repetition
	`, planID, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list calibration attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var attempts []CalibrationAttempt
	for rows.Next() {
		var attempt CalibrationAttempt
		if err := rows.Scan(
			&attempt.PlanID, &attempt.TaskID, &attempt.AttemptSequence, &attempt.Repetition,
			&attempt.Alias, &attempt.Provider, &attempt.Model, &attempt.Effort,
			&attempt.SessionID, &attempt.ProviderSessionID,
			&attempt.AssistantOutputSHA256,
		); err != nil {
			return nil, fmt.Errorf("store: scan calibration attempt: %w", err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate calibration attempts: %w", err)
	}
	return attempts, nil
}
