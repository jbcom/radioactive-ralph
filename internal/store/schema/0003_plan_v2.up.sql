-- ralph.plan/v2 metadata and durable execution provenance.
CREATE TABLE task_metadata (
  plan_id                    TEXT NOT NULL,
  task_id                    TEXT NOT NULL,
  team_path                  TEXT NOT NULL,
  metadata_json              TEXT NOT NULL,
  assigned_alias             TEXT,
  assigned_provider          TEXT,
  assigned_model             TEXT,
  assigned_effort            TEXT,
  assigned_independence_domain TEXT,
  assigned_session_id        TEXT,
  provider_session_id        TEXT,
  calibration_id             TEXT,
  capability_set_json        TEXT,
  completion_evidence_json   TEXT,
  blocked_reason             TEXT,
  PRIMARY KEY (plan_id, task_id),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_metadata_team ON task_metadata(plan_id, team_path);

CREATE TABLE provider_calibrations (
  id                TEXT PRIMARY KEY,
  alias             TEXT NOT NULL UNIQUE,
  provider          TEXT NOT NULL,
  model             TEXT NOT NULL,
  effort            TEXT NOT NULL,
  binary_path       TEXT NOT NULL,
  binary_version    TEXT NOT NULL,
  binary_sha256     TEXT NOT NULL,
  invocation_hash   TEXT NOT NULL,
  inference_domain  TEXT NOT NULL,
  control_domain    TEXT NOT NULL,
  independence_domain TEXT NOT NULL,
  model_digest      TEXT,
  capabilities_json TEXT NOT NULL,
  evidence_json     TEXT NOT NULL,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE task_calibration_attempts (
  plan_id                  TEXT NOT NULL,
  task_id                  TEXT NOT NULL,
  attempt_sequence         INTEGER NOT NULL CHECK (attempt_sequence > 0),
  repetition               INTEGER NOT NULL CHECK (repetition > 0),
  alias                    TEXT NOT NULL,
  provider                 TEXT NOT NULL,
  model                    TEXT NOT NULL,
  effort                   TEXT NOT NULL,
  session_id               TEXT NOT NULL,
  provider_session_id      TEXT,
  assistant_output_sha256  TEXT NOT NULL,
  created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (plan_id, task_id, attempt_sequence, repetition),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE TABLE task_input_reservations (
  plan_id   TEXT NOT NULL,
  task_id   TEXT NOT NULL,
  path      TEXT NOT NULL,
  sha256    TEXT NOT NULL,
  PRIMARY KEY (plan_id, task_id, path),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_input_reservations_path
  ON task_input_reservations(plan_id, path);

CREATE TABLE task_output_reservations (
  plan_id   TEXT NOT NULL,
  task_id   TEXT NOT NULL,
  path      TEXT NOT NULL,
  mode      TEXT NOT NULL CHECK (mode = 'exclusive'),
  PRIMARY KEY (plan_id, task_id, path),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_output_reservations_path
  ON task_output_reservations(plan_id, path);
