-- Roll back plandag schema v1.
-- Dropping plans cascades to every dependent table via ON DELETE CASCADE.

DROP TRIGGER IF EXISTS tasks_updated_at;
DROP TRIGGER IF EXISTS plans_updated_at;

DROP TABLE IF EXISTS task_heartbeats;
DROP TABLE IF EXISTS session_variants;
DROP TABLE IF EXISTS session_plans;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS task_events;
DROP TABLE IF EXISTS parallelism_hints;
DROP TABLE IF EXISTS task_deps;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS analyses;
DROP TABLE IF EXISTS intents;
DROP TABLE IF EXISTS plan_aliases;
DROP TABLE IF EXISTS plans;
