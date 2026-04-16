-- plandag schema v1
-- Stores the Phase 1 (intent), Phase 2 (analysis), Phase 3 (DAG)
-- pipeline emitted by fixit for every user-facing plan, plus the
-- live session/variant heartbeat tables the reaper uses.
--
-- Schema version comment is read by dump/restore to refuse
-- version-skewed imports.
-- schema_version: 1

-- ──────────────────────────────────────────────────────────
-- plans: the registry of every plan across all repos
-- ──────────────────────────────────────────────────────────
CREATE TABLE plans (
  id               TEXT PRIMARY KEY,           -- UUID v7
  slug             TEXT NOT NULL,              -- 'm3-completion'
  title            TEXT NOT NULL,
  repo_path        TEXT,                       -- absolute, may drift
  repo_remote      TEXT,                       -- git remote URL, for portability across clones
  status           TEXT NOT NULL DEFAULT 'draft',
                                               -- draft|active|paused|done|failed_partial|archived|abandoned
  primary_variant  TEXT,                       -- recommended variant: green/professor/etc
  confidence       INTEGER,                    -- fixit's last confidence score (0-100)
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_session_at  DATETIME,
  tags_json        TEXT                        -- JSON array of operator-supplied tags
);

CREATE UNIQUE INDEX plans_slug_per_repo ON plans(repo_path, slug);
CREATE INDEX plans_status ON plans(status);
CREATE INDEX plans_updated ON plans(updated_at DESC);

-- Global aliases let operators refer to plans from any repo via one shortname.
CREATE TABLE plan_aliases (
  alias    TEXT PRIMARY KEY,
  plan_id  TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE
);

-- ──────────────────────────────────────────────────────────
-- intents: Phase 1 artifact. Append-only history per plan.
-- ──────────────────────────────────────────────────────────
CREATE TABLE intents (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id      TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  raw_input    TEXT NOT NULL,                  -- whatever the operator dumped
  sources_json TEXT,                           -- JSON array of URLs / file paths referenced
  captured_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX intents_plan ON intents(plan_id, captured_at DESC);

-- ──────────────────────────────────────────────────────────
-- analyses: Phase 2 artifact. Planning-provider structured output per intent.
-- ──────────────────────────────────────────────────────────
CREATE TABLE analyses (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id      TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  intent_id    INTEGER NOT NULL REFERENCES intents(id) ON DELETE CASCADE,
  model        TEXT NOT NULL,                  -- e.g., 'opus'
  effort       TEXT NOT NULL,                  -- low|medium|high|max
  confidence   INTEGER,                        -- 0-100
  raw_json     TEXT NOT NULL,                  -- full provider output, frozen for replay
  completed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX analyses_plan ON analyses(plan_id, completed_at DESC);

-- ──────────────────────────────────────────────────────────
-- tasks: Phase 3 DAG. The actionable items.
-- ──────────────────────────────────────────────────────────
CREATE TABLE tasks (
  id                   TEXT NOT NULL,           -- stable slug within plan
  plan_id              TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  description          TEXT NOT NULL,
  complexity           TEXT,                    -- S|M|L
  effort               TEXT,                    -- S|M|L
  variant_hint         TEXT,                    -- suggested variant: green/professor/...
  context_boundary     INTEGER NOT NULL DEFAULT 0,
                                                -- 1 = fits alone in one session (Phase-2 hint)
  acceptance_json      TEXT,                    -- JSON array of acceptance criteria
  status               TEXT NOT NULL DEFAULT 'pending',
                                                -- pending|ready|running|done|failed|skipped
                                                -- |decomposed|ready_pending_approval
  assigned_variant     TEXT,                    -- which ralph spawned claimed it
  claimed_by_session   TEXT REFERENCES sessions(id) ON DELETE SET NULL,
  claimed_by_variant_id TEXT REFERENCES session_variants(id) ON DELETE SET NULL,
  retry_count          INTEGER NOT NULL DEFAULT 0,
  reclaim_count        INTEGER NOT NULL DEFAULT 0,
  parent_task_id       TEXT,                    -- for decomposed parents; FK below
  created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plan_id, id),
  FOREIGN KEY (plan_id, parent_task_id) REFERENCES tasks(plan_id, id) ON DELETE SET NULL
);

CREATE INDEX tasks_status ON tasks(plan_id, status);
CREATE INDEX tasks_variant ON tasks(plan_id, assigned_variant) WHERE assigned_variant IS NOT NULL;

-- ──────────────────────────────────────────────────────────
-- task_deps: the DAG edges. Cycle prevention lives in Go.
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
-- parallelism_hints: Phase 2 clusters tasks that can run in
-- parallel. The repo service uses these to batch claim+spawn.
-- ──────────────────────────────────────────────────────────
CREATE TABLE parallelism_hints (
  plan_id  TEXT NOT NULL,
  group_id INTEGER NOT NULL,
  task_id  TEXT NOT NULL,
  PRIMARY KEY (plan_id, group_id, task_id),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

-- ──────────────────────────────────────────────────────────
-- task_events: append-only audit log of every task state change.
-- The repo service and `radioactive_ralph plan history` read from this.
-- ──────────────────────────────────────────────────────────
CREATE TABLE task_events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id     TEXT NOT NULL,
  task_id     TEXT NOT NULL,
  event_type  TEXT NOT NULL,                   -- claimed|started|progress|completed|failed|refined|reclaimed|decomposed|skipped
  variant     TEXT,                            -- which ralph
  session_id  TEXT,                            -- owning provider session
  payload_json TEXT,                           -- free-form JSON for event-specific data
  occurred_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_events_task ON task_events(plan_id, task_id, occurred_at);
CREATE INDEX task_events_session ON task_events(session_id, occurred_at) WHERE session_id IS NOT NULL;

-- ──────────────────────────────────────────────────────────
-- sessions: every attached run or durable repo-service instance that's
-- touched this DB. Reaper deletes stale rows; `radioactive_ralph status` reads
-- from here.
-- ──────────────────────────────────────────────────────────
CREATE TABLE sessions (
  id              TEXT PRIMARY KEY,            -- UUID v7
  mode            TEXT NOT NULL,               -- attached|durable
  transport       TEXT NOT NULL,               -- stdio|socket
  pid             INTEGER,                     -- optional process id for the owning session
  pid_start_time  TEXT,                        -- from /proc or ps, recycling defense
  host            TEXT,                        -- hostname, for multi-machine registries
  started_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_heartbeat  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX sessions_heartbeat ON sessions(last_heartbeat DESC);

-- Which plan(s) is each session currently attached to? One-to-many.
CREATE TABLE session_plans (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  plan_id    TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  attached_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (session_id, plan_id)
);

-- Variant subprocess lifecycles owned by a session.
CREATE TABLE session_variants (
  id                    TEXT PRIMARY KEY,      -- UUID v7
  session_id            TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  variant_name          TEXT NOT NULL,         -- green|professor|etc
  subprocess_pid        INTEGER NOT NULL,
  subprocess_start_time TEXT NOT NULL,
  current_task_id       TEXT,                  -- NULL if idle
  current_plan_id       TEXT,
  started_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_heartbeat        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  status                TEXT NOT NULL DEFAULT 'running',
                                               -- running|idle|terminated|crashed
  FOREIGN KEY (current_plan_id, current_task_id)
    REFERENCES tasks(plan_id, id) ON DELETE SET NULL
);

CREATE INDEX session_variants_session ON session_variants(session_id);
CREATE INDEX session_variants_heartbeat ON session_variants(last_heartbeat DESC);

-- ──────────────────────────────────────────────────────────
-- task_heartbeats: separate high-write table so claims/progress
-- don't bloat task_events with a heartbeat every 30s.
-- ──────────────────────────────────────────────────────────
CREATE TABLE task_heartbeats (
  plan_id    TEXT NOT NULL,
  task_id    TEXT NOT NULL,
  session_variant_id TEXT NOT NULL REFERENCES session_variants(id) ON DELETE CASCADE,
  last_seen  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plan_id, task_id, session_variant_id),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

-- Updated_at triggers — keep plans.updated_at and tasks.updated_at fresh
-- without callers remembering. (Also makes dump output deterministic via
-- always-present CURRENT_TIMESTAMP on mutation.)
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
