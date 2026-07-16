-- a2a_messages: the worker<->orchestrator evidence/message log.
--
-- This is NOT a parallel task store — the existing tasks table (schema v1)
-- remains the durable plan DAG. a2a_messages only records the A2A-vocabulary
-- messages (per .agent-state/decisions.ndjson "a2a-comms-layer") a worker
-- exchanges with the orchestrator while working a task, most importantly
-- the Evidence a worker submits when it believes a task is done. Recording
-- that message here is NOT the same as marking the task done — only
-- internal/orch.VerifyAndComplete may transition a task to done, after
-- re-running the task's acceptance check.
--
-- schema_version: 2

CREATE TABLE a2a_messages (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  worker_id    TEXT REFERENCES workers(id) ON DELETE SET NULL,
  plan_id      TEXT NOT NULL,
  task_id      TEXT NOT NULL,
  role         TEXT NOT NULL,             -- ROLE_AGENT|ROLE_USER (a2a.MessageRole)
  content_json TEXT NOT NULL,             -- the serialized a2a.Message
  occurred_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (plan_id, task_id) REFERENCES tasks(plan_id, id) ON DELETE CASCADE
);

CREATE INDEX a2a_messages_task ON a2a_messages(plan_id, task_id, occurred_at);
CREATE INDEX a2a_messages_worker ON a2a_messages(worker_id, occurred_at);
