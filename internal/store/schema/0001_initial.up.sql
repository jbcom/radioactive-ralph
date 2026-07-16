-- radioactive-ralph user store schema v1.
--
-- ONE user-level SQLite database (XDG data dir) is the durable memory for
-- ALL projects: project identity, DB-resident config, the plan DAG, the
-- append-only event log, worker/session tracking + heartbeats (for the
-- in-store reaper), and spend accounting. Repos stay clean — nothing is
-- committed to a repo by default.
--
-- Ported from the old per-repo internal/plandag + internal/db schemas with:
--   * variants REMOVED (one mutating Ralph; no persona columns) — the old
--     variant_hint / assigned_variant / session_variants become a generic,
--     free-string "worker" tag the orchestrator assigns from plan structure.
--   * project identity via ACCUMULATED FINGERPRINTS, not fragile paths.
--   * DB-resident project config (no committed config dir).
--   * PR #63 safety carried by the Go layer (DSN _txlock=immediate +
--     synchronous(NORMAL); checked RowsAffected; error-logged terminal
--     writes) — not expressible in schema alone.
--
-- schema_version: 1

-- ──────────────────────────────────────────────────────────
-- projects: one row per known project. Identity is the set of
-- fingerprints in project_identifiers, NOT the (drift-prone) path.
-- ──────────────────────────────────────────────────────────
CREATE TABLE projects (
  id              TEXT PRIMARY KEY,              -- UUID v7
  display_name    TEXT,                          -- operator-friendly label
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at    DATETIME
);

-- Accumulated identity fingerprints. A directory that is later git-init'ed
-- gains its git identifier(s) ON TOP of the path identifier, so the same
-- project stays recognized across the git transition and directory moves.
-- kind: 'abs_path' | 'git_root_commit' | 'git_remote' | 'git_worktree_root'.
CREATE TABLE project_identifiers (
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  value       TEXT NOT NULL,
  added_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (kind, value)                      -- a fingerprint maps to at most one project
);

CREATE INDEX project_identifiers_project ON project_identifiers(project_id);

-- DB-resident project config (no committed config dir). Key/value; the
-- virtual-merge layer (viper-backed) composes this with user config and any
-- passed-by-path overrides.
CREATE TABLE project_config (
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL,                     -- JSON-encoded scalar/array/object
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (project_id, key)
);

-- ──────────────────────────────────────────────────────────
-- plans: the plan registry, keyed by project (not repo_path).
-- ──────────────────────────────────────────────────────────
CREATE TABLE plans (
  id               TEXT PRIMARY KEY,             -- UUID v7
  project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  slug             TEXT NOT NULL,
  title            TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'draft',
                                                 -- draft|active|paused|done|failed_partial|archived|abandoned
  source_markdown  TEXT,                         -- the plan markdown (goldmark-parsed at load)
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_session_at  DATETIME,
  tags_json        TEXT
);

CREATE UNIQUE INDEX plans_slug_per_project ON plans(project_id, slug);
CREATE INDEX plans_status ON plans(status);
CREATE INDEX plans_updated ON plans(updated_at DESC);

-- ──────────────────────────────────────────────────────────
-- tasks: the actionable items. Decomposed heuristically from the plan
-- markdown (Phase 6). No variant columns — the orchestrator assigns a
-- worker from plan structure.
-- ──────────────────────────────────────────────────────────
CREATE TABLE tasks (
  id                   TEXT NOT NULL,            -- stable slug within plan
  plan_id              TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  description          TEXT NOT NULL,
  status               TEXT NOT NULL DEFAULT 'pending',
                                                 -- pending|ready|running|done|failed|skipped|decomposed|ready_pending_approval
  parallel_group       INTEGER,                  -- steps sharing a group may run in parallel (unordered list)
  sequence_ordinal     INTEGER,                  -- ordered-list position (sequential), NULL if unordered
  acceptance_json      TEXT,                     -- done-criteria the orchestrator verifies against
  claimed_by_session   TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  claimed_by_worker_id TEXT REFERENCES workers(id) ON DELETE SET NULL,
  retry_count          INTEGER NOT NULL DEFAULT 0,
  reclaim_count        INTEGER NOT NULL DEFAULT 0,
  parent_task_id       TEXT,
  created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plan_id, id),
  FOREIGN KEY (plan_id, parent_task_id) REFERENCES tasks(plan_id, id) ON DELETE SET NULL
);

CREATE INDEX tasks_status ON tasks(plan_id, status);

-- ──────────────────────────────────────────────────────────
-- task_deps: the DAG edges. Cycle prevention lives in Go (AddDep).
-- ──────────────────────────────────────────────────────────
CREATE TABLE task_deps (
  plan_id    TEXT NOT NULL,
  task_id    TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  PRIMARY KEY (plan_id, task_id, depends_on),
  FOREIGN KEY (plan_id, task_id)    REFERENCES tasks(plan_id, id) ON DELETE CASCADE,
  FOREIGN KEY (plan_id, depends_on) REFERENCES tasks(plan_id, id) ON DELETE CASCADE,
  CHECK (task_id != depends_on)
);

CREATE INDEX task_deps_dependent ON task_deps(plan_id, depends_on);

-- ──────────────────────────────────────────────────────────
-- events: the append-only event log (ported from internal/db). Both the
-- old per-repo event log and task_events collapse here, project-scoped.
-- ──────────────────────────────────────────────────────────
CREATE TABLE events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id   TEXT REFERENCES projects(id) ON DELETE CASCADE,
  plan_id      TEXT,
  task_id      TEXT,
  kind         TEXT NOT NULL,                    -- e.g. task.claimed|worker.result|worker.spend|worker.admission_refused
  stream       TEXT,                             -- 'service'|'worker'|...
  actor        TEXT,
  payload_json TEXT,
  occurred_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX events_project ON events(project_id, occurred_at);
CREATE INDEX events_task ON events(plan_id, task_id, occurred_at) WHERE task_id IS NOT NULL;

-- ──────────────────────────────────────────────────────────
-- sessions: each --supervisor instance (or client) touching this DB.
-- The reaper deletes stale rows; status reads from here.
-- ──────────────────────────────────────────────────────────
CREATE TABLE sessions (
  id              TEXT PRIMARY KEY,              -- UUID v7
  role            TEXT NOT NULL,                 -- supervisor|client
  pid             INTEGER,
  pid_start_time  TEXT,                          -- recycling defense
  host            TEXT,
  started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_heartbeat  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX sessions_heartbeat ON sessions(last_heartbeat DESC);

-- ──────────────────────────────────────────────────────────
-- workers: agent subprocess lifecycles owned by a supervisor session.
-- Successor to session_variants — NO persona; carries the provider
-- capability (native_fanout) instead of a variant name.
-- ──────────────────────────────────────────────────────────
CREATE TABLE workers (
  id                    TEXT PRIMARY KEY,        -- UUID v7
  session_id            TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  provider              TEXT NOT NULL,           -- claude|codex|opencode|...
  model                 TEXT,
  native_fanout         INTEGER NOT NULL DEFAULT 0, -- 1 if the CLI/API fans out itself
  subprocess_pid        INTEGER NOT NULL,
  subprocess_start_time TEXT NOT NULL,
  current_plan_id       TEXT,
  current_task_id       TEXT,
  started_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_heartbeat        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  status                TEXT NOT NULL DEFAULT 'running', -- running|idle|terminated|crashed
  FOREIGN KEY (current_plan_id, current_task_id) REFERENCES tasks(plan_id, id) ON DELETE SET NULL
);

CREATE INDEX workers_session ON workers(session_id);
CREATE INDEX workers_heartbeat ON workers(last_heartbeat DESC);

-- ──────────────────────────────────────────────────────────
-- spend: per-worker/model token + cost accounting for spend caps
-- (enforced by the Go layer; carried over from PR #63 semantics).
-- ──────────────────────────────────────────────────────────
CREATE TABLE spend (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id    TEXT REFERENCES projects(id) ON DELETE CASCADE,
  worker_id     TEXT REFERENCES workers(id) ON DELETE SET NULL,
  provider      TEXT NOT NULL,
  model         TEXT,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cached_tokens INTEGER NOT NULL DEFAULT 0,
  cost_usd      REAL NOT NULL DEFAULT 0,
  occurred_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX spend_project ON spend(project_id, occurred_at);

-- Updated-at triggers keep plans/tasks/projects fresh without callers
-- remembering.
CREATE TRIGGER projects_updated_at
AFTER UPDATE ON projects
BEGIN
  UPDATE projects SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER plans_updated_at
AFTER UPDATE ON plans
BEGIN
  UPDATE plans SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TRIGGER tasks_updated_at
AFTER UPDATE ON tasks
BEGIN
  UPDATE tasks SET updated_at = CURRENT_TIMESTAMP
  WHERE plan_id = NEW.plan_id AND id = NEW.id;
END;
