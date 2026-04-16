package plandag

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SessionMode identifies attached/headless vs durable repo-service modes.
type SessionMode string

// Session execution modes.
const (
	SessionModeAttached SessionMode = "attached"
	SessionModeDurable  SessionMode = "durable"
)

// SessionTransport identifies how the operator/runtime reached the session.
type SessionTransport string

// Session transport types.
const (
	SessionTransportStdio  SessionTransport = "stdio"
	SessionTransportSocket SessionTransport = "socket"
)

// SessionOpts configures CreateSession.
type SessionOpts struct {
	ID           string // optional; caller may pass an existing UUID. Empty → auto.
	Mode         SessionMode
	Transport    SessionTransport
	PID          int
	PIDStartTime string
	Host         string
}

// CreateSession inserts a session row. Returns the session id.
// The row lifetime matches one attached run or durable repo-service
// process.
func (s *Store) CreateSession(ctx context.Context, o SessionOpts) (string, error) {
	id := o.ID
	if id == "" {
		id = s.uuid()
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)

	var pid sql.NullInt64
	if o.PID > 0 {
		pid = sql.NullInt64{Int64: int64(o.PID), Valid: true}
	}
	var pidStart sql.NullString
	if o.PIDStartTime != "" {
		pidStart = sql.NullString{String: o.PIDStartTime, Valid: true}
	}
	var host sql.NullString
	if o.Host != "" {
		host = sql.NullString{String: o.Host, Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions(id, mode, transport, pid, pid_start_time, host, started_at, last_heartbeat)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, string(o.Mode), string(o.Transport), pid, pidStart, host, now, now)
	if err != nil {
		return "", fmt.Errorf("plandag: insert session: %w", err)
	}
	return id, nil
}

// HeartbeatSession refreshes last_heartbeat for a session. Called
// periodically by the attached run or durable repo service. Reaper
// uses staleness to detect dead sessions.
func (s *Store) HeartbeatSession(ctx context.Context, sessionID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_heartbeat = ? WHERE id = ?`, now, sessionID)
	return err
}

// CloseSession removes a session row. FK cascades clear its variants
// and attach rows.
func (s *Store) CloseSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

// SessionVariantOpts configures CreateSessionVariant.
type SessionVariantOpts struct {
	SessionID           string
	VariantName         string
	SubprocessPID       int
	SubprocessStartTime string
}

// CreateSessionVariant registers a newly-spawned ralph subprocess
// against a session. Returns the variant row id.
func (s *Store) CreateSessionVariant(ctx context.Context, o SessionVariantOpts) (string, error) {
	if o.SessionID == "" || o.VariantName == "" || o.SubprocessPID <= 0 || o.SubprocessStartTime == "" {
		return "", fmt.Errorf("plandag: SessionID, VariantName, SubprocessPID, SubprocessStartTime all required")
	}
	id := s.uuid()
	now := s.clock.Now().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_variants(
			id, session_id, variant_name, subprocess_pid, subprocess_start_time,
			started_at, last_heartbeat, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'running')
	`, id, o.SessionID, o.VariantName, o.SubprocessPID, o.SubprocessStartTime, now, now)
	if err != nil {
		return "", fmt.Errorf("plandag: insert session_variant: %w", err)
	}
	return id, nil
}

// SetSessionVariantTask updates the currently assigned plan/task for one
// session_variant row and refreshes its heartbeat.
func (s *Store) SetSessionVariantTask(ctx context.Context, sessionVariantID, planID, taskID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE session_variants
		SET current_plan_id = ?,
		    current_task_id = ?,
		    status = 'running',
		    last_heartbeat = ?
		WHERE id = ?
	`, nullIfEmpty(planID), nullIfEmpty(taskID), now, sessionVariantID)
	if err != nil {
		return fmt.Errorf("plandag: set session variant task: %w", err)
	}
	return nil
}

// HeartbeatSessionVariant refreshes one worker row's heartbeat.
func (s *Store) HeartbeatSessionVariant(ctx context.Context, sessionVariantID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE session_variants
		SET last_heartbeat = ?
		WHERE id = ?
	`, now, sessionVariantID)
	return err
}

// ClearSessionVariantTask clears the active task from one worker row and marks
// it idle or terminated.
func (s *Store) ClearSessionVariantTask(ctx context.Context, sessionVariantID, status string) error {
	if status == "" {
		status = "idle"
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE session_variants
		SET current_plan_id = NULL,
		    current_task_id = NULL,
		    status = ?,
		    last_heartbeat = ?
		WHERE id = ?
	`, status, now, sessionVariantID)
	if err != nil {
		return fmt.Errorf("plandag: clear session variant task: %w", err)
	}
	return nil
}

// AttachPlan records which session is attached to which plan. Used by
// `radioactive_ralph status` to enumerate active runtime ownership.
func (s *Store) AttachPlan(ctx context.Context, sessionID, planID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO session_plans(session_id, plan_id, attached_at)
		VALUES (?, ?, ?)
	`, sessionID, planID, now)
	return err
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
