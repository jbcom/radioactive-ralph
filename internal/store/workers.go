package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SessionOpts configures CreateSession.
type SessionOpts struct {
	ID           string // optional; caller may pass an existing UUID. Empty → auto.
	Role         string // supervisor|client
	PID          int
	PIDStartTime string
	Host         string
}

// CreateSession inserts a session row. Returns the session id. The row
// lifetime matches one supervisor or client process attached to this DB.
func (s *Store) CreateSession(ctx context.Context, o SessionOpts) (string, error) {
	if o.Role == "" {
		return "", fmt.Errorf("store: Role required")
	}
	id := o.ID
	if id == "" {
		id = s.uuid()
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)

	var pid sql.NullInt64
	if o.PID > 0 {
		pid = sql.NullInt64{Int64: int64(o.PID), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions(id, role, pid, pid_start_time, host, started_at, last_heartbeat)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, o.Role, pid, nullIfEmpty(o.PIDStartTime), nullIfEmpty(o.Host), now, now)
	if err != nil {
		return "", fmt.Errorf("store: insert session: %w", err)
	}
	return id, nil
}

// HeartbeatSession refreshes last_heartbeat for a session. Called
// periodically by the durable supervisor. The reaper uses staleness to
// detect dead sessions.
func (s *Store) HeartbeatSession(ctx context.Context, sessionID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_heartbeat = ? WHERE id = ?`, now, sessionID)
	if err != nil {
		return fmt.Errorf("store: heartbeat session: %w", err)
	}
	return nil
}

// CloseSession removes a session row. FK cascades clear its workers.
func (s *Store) CloseSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("store: close session: %w", err)
	}
	return nil
}

// WorkerOpts configures CreateWorker.
type WorkerOpts struct {
	SessionID           string
	Provider            string
	Model               string
	NativeFanout        bool
	SubprocessPID       int
	SubprocessStartTime string
}

// CreateWorker registers a newly-spawned agent subprocess against a
// session. Returns the worker row id. Successor to plandag's
// CreateSessionVariant — no persona; carries the provider capability
// (native_fanout) instead of a variant name (§9/§10 of the design).
func (s *Store) CreateWorker(ctx context.Context, o WorkerOpts) (string, error) {
	if o.SessionID == "" || o.Provider == "" || o.SubprocessPID <= 0 || o.SubprocessStartTime == "" {
		return "", fmt.Errorf("store: SessionID, Provider, SubprocessPID, SubprocessStartTime all required")
	}
	id := s.uuid()
	now := s.clock.Now().UTC().Format(time.RFC3339)
	fanout := 0
	if o.NativeFanout {
		fanout = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workers(
			id, session_id, provider, model, native_fanout,
			subprocess_pid, subprocess_start_time,
			started_at, last_heartbeat, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'running')
	`, id, o.SessionID, o.Provider, nullIfEmpty(o.Model), fanout,
		o.SubprocessPID, o.SubprocessStartTime, now, now)
	if err != nil {
		return "", fmt.Errorf("store: insert worker: %w", err)
	}
	return id, nil
}

// SetWorkerTask updates the currently assigned plan/task for one worker row
// and refreshes its heartbeat.
func (s *Store) SetWorkerTask(ctx context.Context, workerID, planID, taskID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE workers
		SET current_plan_id = ?,
		    current_task_id = ?,
		    status = 'running',
		    last_heartbeat = ?
		WHERE id = ?
	`, nullIfEmpty(planID), nullIfEmpty(taskID), now, workerID)
	if err != nil {
		return fmt.Errorf("store: set worker task: %w", err)
	}
	return nil
}

// HeartbeatWorker refreshes one worker row's heartbeat.
func (s *Store) HeartbeatWorker(ctx context.Context, workerID string) error {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE workers SET last_heartbeat = ? WHERE id = ?
	`, now, workerID)
	if err != nil {
		return fmt.Errorf("store: heartbeat worker: %w", err)
	}
	return nil
}

// CountRunningWorkers returns how many worker rows are currently
// status='running' — i.e. actively assigned a task, not idle/terminated/
// crashed. Used by the supervisor's HandleStatus to report a real
// ActiveWorkers count sourced from the store rather than an in-process
// map that only the dispatcher owning the pty could ever populate.
func (s *Store) CountRunningWorkers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers WHERE status = 'running'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count running workers: %w", err)
	}
	return n, nil
}

// ClearWorkerTask clears the active task from one worker row and marks it
// idle or terminated (status defaults to "idle" when empty).
func (s *Store) ClearWorkerTask(ctx context.Context, workerID, status string) error {
	if status == "" {
		status = "idle"
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE workers
		SET current_plan_id = NULL,
		    current_task_id = NULL,
		    status = ?,
		    last_heartbeat = ?
		WHERE id = ?
	`, status, now, workerID)
	if err != nil {
		return fmt.Errorf("store: clear worker task: %w", err)
	}
	return nil
}
