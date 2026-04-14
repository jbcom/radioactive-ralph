-- Initial schema for radioactive-ralph's per-repo event log.
--
-- Opened in WAL mode with foreign keys on. The daemon is the sole
-- writer; readers (ralph status, ralph attach) open separate
-- connections.

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

-- events is an append-only log. Every interesting thing the supervisor
-- does goes here: spawns, user messages sent to managed sessions, stream-json
-- events received, session deaths, resumes, commits authored, PRs opened,
-- spend tracked, errors.
--
-- payload_parsed is the structured form the supervisor understands;
-- payload_raw is the exact bytes received from stream-json so we can
-- replay old events through a new parser if the Claude Code wire format
-- drifts.
CREATE TABLE IF NOT EXISTS events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  ts           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  stream       TEXT NOT NULL,    -- "task:<uuid>" | "session:<uuid>" | "repo:<hash>" | "supervisor"
  kind         TEXT NOT NULL,    -- "task.enqueued" | "session.spawned" | "message.user" | ...
  actor        TEXT NOT NULL,    -- "daemon" | "session:<uuid>" | "operator"
  payload_parsed TEXT,           -- JSON blob the supervisor understands (may be NULL if raw-only)
  payload_raw  BLOB              -- exact bytes received, for forward-compat replay
) STRICT;

CREATE INDEX IF NOT EXISTS events_stream_ts ON events (stream, ts);
CREATE INDEX IF NOT EXISTS events_kind_ts   ON events (kind, ts);

-- tasks is the queue of work items. Rows transition between states as
-- the supervisor picks them up, assigns them to sessions, and finishes
-- (or abandons) them.
CREATE TABLE IF NOT EXISTS tasks (
  id             TEXT PRIMARY KEY,                      -- uuid
  description    TEXT NOT NULL,
  priority       INTEGER NOT NULL DEFAULT 5,
  status         TEXT NOT NULL DEFAULT 'queued',         -- queued | running | blocked | done | failed
  worktree_path  TEXT,                                   -- only set when running
  claimed_by     TEXT,                                   -- session uuid, only set when running
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at     TEXT
) STRICT;

CREATE INDEX IF NOT EXISTS tasks_status_priority ON tasks (status, priority);

-- FTS5 index over task descriptions for dedup. When the supervisor
-- enqueues a new task it queries this table to check whether a
-- near-identical task is already pending/running.
CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts USING fts5(
  description,
  content='tasks',
  content_rowid='rowid',
  tokenize='porter unicode61'
);

-- Keep FTS in sync with tasks via triggers.
CREATE TRIGGER IF NOT EXISTS tasks_fts_insert AFTER INSERT ON tasks BEGIN
  INSERT INTO tasks_fts (rowid, description) VALUES (new.rowid, new.description);
END;
CREATE TRIGGER IF NOT EXISTS tasks_fts_delete AFTER DELETE ON tasks BEGIN
  INSERT INTO tasks_fts (tasks_fts, rowid, description) VALUES ('delete', old.rowid, old.description);
END;
CREATE TRIGGER IF NOT EXISTS tasks_fts_update AFTER UPDATE ON tasks BEGIN
  INSERT INTO tasks_fts (tasks_fts, rowid, description) VALUES ('delete', old.rowid, old.description);
  INSERT INTO tasks_fts (rowid, description) VALUES (new.rowid, new.description);
END;

-- sessions tracks every managed Claude subprocess the supervisor has
-- spawned or adopted. One row per session UUID. Sessions may be long-lived
-- across many task assignments; the tasks table references claimed_by =
-- sessions.uuid while a task is in-flight.
CREATE TABLE IF NOT EXISTS sessions (
  uuid            TEXT PRIMARY KEY,
  variant         TEXT NOT NULL,       -- "green" | "grey" | ...
  worktree_path   TEXT,
  pid             INTEGER,
  model           TEXT,                -- "claude-haiku-4-5-..." etc
  stage           TEXT,                -- "plan" | "execute" | "reflect" | ...
  started_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  exited_at       TEXT,
  exit_reason     TEXT                 -- "clean" | "context-exhausted" | "rate-limited" | "crashed" | "killed"
) STRICT;

CREATE INDEX IF NOT EXISTS sessions_variant ON sessions (variant);

-- spend tracks accumulated API usage per (session, model) for spend-cap
-- enforcement. Populated by the supervisor's event-loop consumer as
-- stream-json result events carry usage fields.
CREATE TABLE IF NOT EXISTS spend (
  session_uuid   TEXT NOT NULL,
  model          TEXT NOT NULL,
  input_tokens   INTEGER NOT NULL DEFAULT 0,
  output_tokens  INTEGER NOT NULL DEFAULT 0,
  cached_input   INTEGER NOT NULL DEFAULT 0,
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (session_uuid, model),
  FOREIGN KEY (session_uuid) REFERENCES sessions (uuid) ON DELETE CASCADE
) STRICT;

-- schema_migrations tracks which migrations have been applied. Present
-- so future schema changes (002_*.sql, 003_*.sql, ...) can be ordered
-- idempotently on startup.
CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INTEGER PRIMARY KEY,
  applied_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;

INSERT OR IGNORE INTO schema_migrations (version) VALUES (1);
