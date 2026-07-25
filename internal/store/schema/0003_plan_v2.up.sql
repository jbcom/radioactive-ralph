-- ralph.plan/v2 metadata and durable execution provenance.
CREATE TABLE task_metadata (
  plan_id                    TEXT NOT NULL,
  task_id                    TEXT NOT NULL,
  team_path                  TEXT NOT NULL,
  metadata_json              TEXT NOT NULL,
  assigned_provider          TEXT,
  completion_evidence_json   TEXT,
  blocked_reason             TEXT,
  PRIMARY KEY (plan_id, task_id),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX task_metadata_team ON task_metadata(plan_id, team_path);

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
