// Package db is radioactive-ralph's per-repo SQLite event log.
//
// Every interesting thing the repo service does is recorded as an event
// here: session spawns, user messages injected into managed provider
// subprocesses, stream-json events received, session deaths, resumes,
// commits, PRs, errors. The database is an append-only log the runtime
// replays on startup to rebuild in-memory state.
//
// SQLite is opened with journal_mode=WAL so the runtime (single
// writer) and status/attach readers (many readers) can coexist without
// locking. foreign_keys=ON for referential integrity on sessions<->spend.
// busy_timeout=5000 so brief writer contention during reader checkpoint
// doesn't crash the service.
//
// Dedup uses SQLite's built-in FTS5 virtual table over task descriptions.
// Full semantic search (sqlite-vec) is intentionally out of scope for
// the current runtime because it requires an embeddings pipeline we do
// not want to own yet.
package db

import (
	"context"
	"database/sql"
	_ "embed" // for schema SQL
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGo
)

//go:embed schema/001_initial.sql
var schemaInitial string

// DriverName is the modernc.org/sqlite driver identifier. Exposed so
// tests can open in-memory databases directly.
const DriverName = "sqlite"

// DB is the package's wrapper around *sql.DB with the runtime's
// helper methods attached. Construct via Open.
type DB struct {
	conn *sql.DB
	path string
}

// Open opens the SQLite database at path (creating it if absent),
// applies pending migrations, and returns a *DB ready for use.
//
// path is typically xdg.Paths.StateDB. An empty string opens an
// in-memory database, which is what tests want.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := path
	if dsn == "" {
		// Shared in-memory so multiple DB objects can see the same data
		// within a single process; unnamed in-memory is per-connection.
		dsn = "file::memory:?mode=memory&cache=shared"
	} else {
		// URI form so we can pass pragmas via query params. The driver
		// defaults to "rwc" which creates the file if missing.
		dsn = "file:" + filepath.Clean(path) + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	}
	conn, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	// Ping early so migration failures surface here, not on first query.
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("db: ping %q: %w", path, err)
	}

	db := &DB{conn: conn, path: path}
	if err := db.migrate(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the underlying connection pool.
func (d *DB) Close() error {
	return d.conn.Close()
}

// Path returns the on-disk path of the database (empty string for
// in-memory).
func (d *DB) Path() string {
	return d.path
}

// Conn returns the underlying *sql.DB. Exposed for advanced callers
// (integration tests that want to assert schema directly); most
// runtime code should use the typed methods on DB.
func (d *DB) Conn() *sql.DB {
	return d.conn
}

// migrate applies any pending migrations in schema/*.sql.
//
// The embedded schema is a single file today; if we add 002_*.sql
// in the future this function grows a sorted-list driver. Both the
// initial migration and any future ones are idempotent (every CREATE
// and INSERT uses OR IGNORE or IF NOT EXISTS).
func (d *DB) migrate(ctx context.Context) error {
	if _, err := d.conn.ExecContext(ctx, schemaInitial); err != nil {
		return fmt.Errorf("db: apply initial schema: %w", err)
	}
	return nil
}

// ---- events API ---------------------------------------------------------

// Event is the structured form of an entry in the events table.
// PayloadRaw holds the exact bytes received from stream-json (nil if
// the event originated inside the repo service).
type Event struct {
	ID            int64
	Timestamp     time.Time
	Stream        string
	Kind          string
	Actor         string
	PayloadParsed any // marshalled to JSON before insert
	PayloadRaw    []byte
}

// Append inserts an event. PayloadParsed is JSON-encoded if non-nil.
func (d *DB) Append(ctx context.Context, e Event) (int64, error) {
	var parsedJSON sql.NullString
	if e.PayloadParsed != nil {
		data, err := json.Marshal(e.PayloadParsed)
		if err != nil {
			return 0, fmt.Errorf("db: marshal payload: %w", err)
		}
		parsedJSON.Valid = true
		parsedJSON.String = string(data)
	}

	res, err := d.conn.ExecContext(ctx,
		`INSERT INTO events (stream, kind, actor, payload_parsed, payload_raw)
		 VALUES (?, ?, ?, ?, ?)`,
		e.Stream, e.Kind, e.Actor, parsedJSON, e.PayloadRaw,
	)
	if err != nil {
		return 0, fmt.Errorf("db: insert event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("db: last insert id: %w", err)
	}
	return id, nil
}

// Replay iterates events in ID order (which is also approximately
// timestamp order) and yields them via the callback. The callback's
// returned error aborts iteration and is propagated to the caller.
//
// afterID lets the caller resume from a previous replay's last-seen ID.
// Pass 0 to iterate from the beginning.
func (d *DB) Replay(ctx context.Context, afterID int64, fn func(Event) error) error {
	rows, err := d.conn.QueryContext(ctx,
		`SELECT id, ts, stream, kind, actor, payload_parsed, payload_raw
		 FROM events WHERE id > ? ORDER BY id ASC`,
		afterID,
	)
	if err != nil {
		return fmt.Errorf("db: query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			e         Event
			tsStr     string
			parsedRaw sql.NullString
			rawBlob   sql.RawBytes
		)
		if err := rows.Scan(&e.ID, &tsStr, &e.Stream, &e.Kind, &e.Actor, &parsedRaw, &rawBlob); err != nil {
			return fmt.Errorf("db: scan event: %w", err)
		}
		t, err := time.Parse("2006-01-02T15:04:05.999Z", tsStr)
		if err != nil {
			// Fall back to RFC3339 without fractional seconds.
			if t2, err2 := time.Parse(time.RFC3339, tsStr); err2 == nil {
				t = t2
			}
		}
		e.Timestamp = t
		if parsedRaw.Valid {
			// Preserve the raw JSON string; the caller decides whether
			// to unmarshal and into what type.
			e.PayloadParsed = json.RawMessage(parsedRaw.String)
		}
		if len(rawBlob) > 0 {
			e.PayloadRaw = append([]byte(nil), rawBlob...)
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ---- tasks API ---------------------------------------------------------

// TaskStatus values map to the tasks.status column.
const (
	TaskQueued  = "queued"
	TaskRunning = "running"
	TaskBlocked = "blocked"
	TaskDone    = "done"
	TaskFailed  = "failed"
)

// Task is a queued unit of work.
type Task struct {
	ID           string
	Description  string
	Priority     int
	Status       string
	WorktreePath string
	ClaimedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EnqueueTask inserts a new task and returns its ID. If a near-duplicate
// description already exists with status in (queued, running), the existing
// task ID is returned and a new row is NOT inserted. Dedup uses FTS5's
// MATCH against the normalised description.
func (d *DB) EnqueueTask(ctx context.Context, id, description string, priority int) (string, bool, error) {
	if id == "" {
		return "", false, errors.New("db: task id required")
	}
	if description == "" {
		return "", false, errors.New("db: task description required")
	}

	// Dedup check. FTS5 wants a quoted phrase for exact-ish matching.
	query := `SELECT t.id FROM tasks t
	          JOIN tasks_fts f ON f.rowid = t.rowid
	          WHERE f.description MATCH ?
	            AND t.status IN (?, ?)
	          LIMIT 1`
	var existingID string
	phrase := ftsPhrase(description)
	err := d.conn.QueryRowContext(ctx, query, phrase, TaskQueued, TaskRunning).Scan(&existingID)
	if err == nil && existingID != "" {
		return existingID, false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// FTS query errors shouldn't block inserts.
		// fall through; worst case we insert a duplicate-looking task
		_ = err
	}

	_, err = d.conn.ExecContext(ctx,
		`INSERT INTO tasks (id, description, priority, status)
		 VALUES (?, ?, ?, 'queued')`,
		id, description, priority,
	)
	if err != nil {
		return "", false, fmt.Errorf("db: insert task: %w", err)
	}
	return id, true, nil
}

// ClaimTask marks a queued task as running and sets the worktree + session.
func (d *DB) ClaimTask(ctx context.Context, taskID, worktreePath, sessionUUID string) error {
	res, err := d.conn.ExecContext(ctx,
		`UPDATE tasks
		 SET status = ?, worktree_path = ?, claimed_by = ?,
		     updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		 WHERE id = ? AND status = ?`,
		TaskRunning, worktreePath, sessionUUID, taskID, TaskQueued,
	)
	if err != nil {
		return fmt.Errorf("db: claim task: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("db: task %q not queued", taskID)
	}
	return nil
}

// FinishTask marks a task as done or failed.
func (d *DB) FinishTask(ctx context.Context, taskID string, success bool) error {
	status := TaskDone
	if !success {
		status = TaskFailed
	}
	_, err := d.conn.ExecContext(ctx,
		`UPDATE tasks
		 SET status = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		 WHERE id = ?`,
		status, taskID,
	)
	if err != nil {
		return fmt.Errorf("db: finish task: %w", err)
	}
	return nil
}

// ListTasks returns tasks matching the given statuses, most recent first.
// Pass nil or empty statuses to return all statuses.
func (d *DB) ListTasks(ctx context.Context, statuses ...string) ([]Task, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if len(statuses) == 0 {
		rows, err = d.conn.QueryContext(ctx,
			`SELECT id, description, priority, status,
			        COALESCE(worktree_path, ''), COALESCE(claimed_by, ''),
			        created_at, COALESCE(updated_at, created_at)
			 FROM tasks ORDER BY created_at DESC`,
		)
	} else {
		// placeholders is literal "?,?,?" — statuses values are bound,
		// not interpolated. Not a SQL-injection risk.
		placeholders := strings.Repeat(",?", len(statuses))[1:]
		//nolint:gosec // placeholders are literal ?, statuses are bound params
		query := `SELECT id, description, priority, status,
		                 COALESCE(worktree_path, ''), COALESCE(claimed_by, ''),
		                 created_at, COALESCE(updated_at, created_at)
		          FROM tasks WHERE status IN (` + placeholders + `)
		          ORDER BY priority ASC, created_at DESC`
		args := make([]any, len(statuses))
		for i, s := range statuses {
			args[i] = s
		}
		rows, err = d.conn.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("db: list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Task
	for rows.Next() {
		var (
			t               Task
			createdStr, upd string
		)
		if err := rows.Scan(&t.ID, &t.Description, &t.Priority, &t.Status,
			&t.WorktreePath, &t.ClaimedBy, &createdStr, &upd); err != nil {
			return nil, fmt.Errorf("db: scan task: %w", err)
		}
		t.CreatedAt, _ = time.Parse("2006-01-02T15:04:05.999Z", createdStr)
		t.UpdatedAt, _ = time.Parse("2006-01-02T15:04:05.999Z", upd)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- sessions API ------------------------------------------------------

// Session is the row shape of the sessions table.
type Session struct {
	UUID         string
	Variant      string
	WorktreePath string
	PID          int
	Model        string
	Stage        string
	StartedAt    time.Time
	ExitedAt     *time.Time
	ExitReason   string
}

// InsertSession records a newly-spawned managed session.
func (d *DB) InsertSession(ctx context.Context, s Session) error {
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO sessions (uuid, variant, worktree_path, pid, model, stage)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.UUID, s.Variant, s.WorktreePath, s.PID, s.Model, s.Stage,
	)
	if err != nil {
		return fmt.Errorf("db: insert session: %w", err)
	}
	return nil
}

// MarkSessionExited records why and when a session ended.
func (d *DB) MarkSessionExited(ctx context.Context, uuid, reason string) error {
	_, err := d.conn.ExecContext(ctx,
		`UPDATE sessions
		 SET exited_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
		     exit_reason = ?
		 WHERE uuid = ?`,
		reason, uuid,
	)
	if err != nil {
		return fmt.Errorf("db: mark session exited: %w", err)
	}
	return nil
}

// ActiveSessions returns sessions that haven't exited yet.
func (d *DB) ActiveSessions(ctx context.Context) ([]Session, error) {
	rows, err := d.conn.QueryContext(ctx,
		`SELECT uuid, variant, COALESCE(worktree_path, ''),
		        COALESCE(pid, 0), COALESCE(model, ''), COALESCE(stage, ''),
		        started_at
		 FROM sessions WHERE exited_at IS NULL
		 ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("db: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Session
	for rows.Next() {
		var s Session
		var startStr string
		if err := rows.Scan(&s.UUID, &s.Variant, &s.WorktreePath,
			&s.PID, &s.Model, &s.Stage, &startStr); err != nil {
			return nil, fmt.Errorf("db: scan session: %w", err)
		}
		s.StartedAt, _ = time.Parse("2006-01-02T15:04:05.999Z", startStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- spend API ---------------------------------------------------------

// AccumulateSpend upserts token counts for a (session, model) pair.
// Input/output/cached values are additive — each call adds to the
// existing totals.
func (d *DB) AccumulateSpend(ctx context.Context, sessionUUID, model string, input, output, cached int64) error {
	_, err := d.conn.ExecContext(ctx,
		`INSERT INTO spend (session_uuid, model, input_tokens, output_tokens, cached_input)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(session_uuid, model) DO UPDATE SET
		   input_tokens  = input_tokens  + excluded.input_tokens,
		   output_tokens = output_tokens + excluded.output_tokens,
		   cached_input  = cached_input  + excluded.cached_input,
		   updated_at    = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		sessionUUID, model, input, output, cached,
	)
	if err != nil {
		return fmt.Errorf("db: accumulate spend: %w", err)
	}
	return nil
}

// Spend is the row shape of the spend table.
type Spend struct {
	SessionUUID  string
	Model        string
	InputTokens  int64
	OutputTokens int64
	CachedInput  int64
}

// SpendBySession returns all (model, spend) entries for one session.
func (d *DB) SpendBySession(ctx context.Context, sessionUUID string) ([]Spend, error) {
	rows, err := d.conn.QueryContext(ctx,
		`SELECT session_uuid, model, input_tokens, output_tokens, cached_input
		 FROM spend WHERE session_uuid = ?`,
		sessionUUID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: query spend: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Spend
	for rows.Next() {
		var s Spend
		if err := rows.Scan(&s.SessionUUID, &s.Model, &s.InputTokens,
			&s.OutputTokens, &s.CachedInput); err != nil {
			return nil, fmt.Errorf("db: scan spend: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- helpers -----------------------------------------------------------

// ftsPhrase prepares a string for FTS5 MATCH by stripping characters
// that FTS treats as operators and wrapping in a phrase query. We
// deliberately don't do fuzzy matching — we want high-precision dedup
// (nearly identical descriptions) and high-recall (miss a dupe and
// you just get two tasks that do the same thing).
func ftsPhrase(s string) string {
	// Strip FTS5 operator characters.
	replacer := strings.NewReplacer(
		`"`, "", "'", "", "(", "", ")", "",
		"^", "", "-", " ", "+", " ", "*", "",
	)
	cleaned := strings.TrimSpace(replacer.Replace(s))
	if cleaned == "" {
		return `""`
	}
	return `"` + cleaned + `"`
}
