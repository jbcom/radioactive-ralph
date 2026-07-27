-- Durable per-task execution provenance, path reservations, and provider
-- calibration records.
--
-- The plan DAG itself is NOT here: task_deps in 0001_initial already holds the
-- edges, with cycle prevention in Go (AddDep) and readiness resolved by the
-- NOT EXISTS walk in Ready/ClaimNextReady. This migration adds the metadata a
-- graph-walking dispatcher needs alongside those edges, not a second graph.

CREATE TABLE task_metadata (
  plan_id                      TEXT NOT NULL,
  task_id                      TEXT NOT NULL,
  -- group_path is the task's leaf-group identity as a dotted StepRef path
  -- ("0.2"). It is load-bearing for dispatch, not descriptive: native fan-out
  -- delegates a whole partition to ONE provider under one group heading, so a
  -- ready wave must be partitioned by compatible group before it can fan out.
  -- Persisting it is what lets dispatch stop re-parsing the plan markdown to
  -- recover positional information.
  group_path                   TEXT NOT NULL,
  team_path                    TEXT NOT NULL,
  metadata_json                TEXT NOT NULL,
  assigned_alias               TEXT,
  assigned_provider            TEXT,
  assigned_model               TEXT,
  assigned_effort              TEXT,
  assigned_independence_domain TEXT,
  assigned_session_id          TEXT,
  provider_session_id          TEXT,
  calibration_id               TEXT,
  capability_set_json          TEXT,
  completion_evidence_json     TEXT,
  blocked_reason               TEXT,
  PRIMARY KEY (plan_id, task_id),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE,
  -- BindTaskCalibration writes this from a caller-supplied string. Without a
  -- referential constraint an invalid or stale calibration id binds durably
  -- with nothing to catch it, and a bound calibration is meant to be the
  -- immutable record of how a task was executed. SQLite allows referencing a
  -- table defined later in the same script, so no reordering is needed.
  FOREIGN KEY (calibration_id) REFERENCES provider_calibrations(id)
);

CREATE INDEX task_metadata_team ON task_metadata(plan_id, team_path);
CREATE INDEX task_metadata_group ON task_metadata(plan_id, group_path);

CREATE TABLE provider_calibrations (
  id                  TEXT PRIMARY KEY,
  alias               TEXT NOT NULL UNIQUE,
  provider            TEXT NOT NULL,
  model               TEXT NOT NULL,
  effort              TEXT NOT NULL,
  binary_path         TEXT NOT NULL,
  binary_version      TEXT NOT NULL,
  binary_sha256       TEXT NOT NULL,
  invocation_hash     TEXT NOT NULL,
  inference_domain    TEXT NOT NULL,
  control_domain      TEXT NOT NULL,
  independence_domain TEXT NOT NULL,
  model_digest        TEXT,
  capabilities_json   TEXT NOT NULL,
  evidence_json       TEXT NOT NULL,
  created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE task_calibration_attempts (
  plan_id                 TEXT NOT NULL,
  task_id                 TEXT NOT NULL,
  attempt_sequence        INTEGER NOT NULL CHECK (attempt_sequence > 0),
  repetition              INTEGER NOT NULL CHECK (repetition > 0),
  alias                   TEXT NOT NULL,
  provider                TEXT NOT NULL,
  model                   TEXT NOT NULL,
  effort                  TEXT NOT NULL,
  session_id              TEXT NOT NULL,
  provider_session_id     TEXT,
  assistant_output_sha256 TEXT NOT NULL,
  created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plan_id, task_id, attempt_sequence, repetition),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE TABLE task_input_reservations (
  plan_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  path    TEXT NOT NULL,
  sha256  TEXT NOT NULL,
  PRIMARY KEY (plan_id, task_id, path),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_input_reservations_path
  ON task_input_reservations(plan_id, path);

CREATE TABLE task_output_reservations (
  plan_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  path    TEXT NOT NULL,
  mode    TEXT NOT NULL CHECK (mode = 'exclusive'),
  PRIMARY KEY (plan_id, task_id, path),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_output_reservations_path
  ON task_output_reservations(plan_id, path);
