package plandag

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SessionMode identifies portable-stdio vs durable-HTTP modes.
type SessionMode string

// Session execution modes.
const (
	SessionModePortable SessionMode = "portable"
	SessionModeDurable  SessionMode = "durable"
)

// SessionTransport identifies stdio / http / sse transports.
type SessionTransport string

// Session transport types.
const (
	SessionTransportStdio SessionTransport = "stdio"
	SessionTransportHTTP  SessionTransport = "http"
	SessionTransportSSE   SessionTransport = "sse"
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
// Called by the MCP server on startup; the session's lifetime is
// the lifetime of one server process.
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
// periodically by the server. Reaper uses staleness to detect dead
// sessions.
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

// AttachPlan records which session is watching which plan. Used by
// `ralph status` to enumerate active supervision.
func (s *Store) AttachPlan(ctx context.Context, sessionID, planID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO session_plans(session_id, plan_id, attached_at)
		VALUES (?, ?, ?)
	`, sessionID, planID, now)
	return err
}
