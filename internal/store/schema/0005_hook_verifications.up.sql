-- Durable, secret-blind state for asynchronous managed-provider Stop checks.
-- Provider payloads never enter this table; only Ralph-owned identifiers and a
-- finite state are persisted.
CREATE TABLE hook_verifications (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  plan_id    TEXT NOT NULL,
  task_id    TEXT NOT NULL,
  state      TEXT NOT NULL CHECK (state IN ('pending', 'passed', 'failed')),
  updated_at TEXT NOT NULL,
  PRIMARY KEY (session_id, plan_id, task_id),
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX hook_verifications_task
  ON hook_verifications(plan_id, task_id);
