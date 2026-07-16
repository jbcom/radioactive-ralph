# Review Scope

## Target

The entire `radioactive-ralph` repository at `/Users/jbogaty/src/jbcom/radioactive-ralph` — a Go-based, repo-scoped runtime for AI-assisted software work. No target argument was supplied and the working tree is clean on `main`, so the full codebase is in scope, with primary weight on the Go source.

## Files

Primary (Go source, 142 files / ~22k LOC, 42 test files):

- `cmd/radioactive_ralph/` — CLI entry point and subcommands (21 files)
- `internal/config/` — repo config + local overrides
- `internal/db/` — event log
- `internal/doctor/` — environment checks
- `internal/fixit/` — fixit planning pipeline (15 files)
- `internal/initcmd/` — repo bootstrap
- `internal/ipc/` — socket protocol and client/server (8 files)
- `internal/plandag/` — durable SQLite plan DAG (19 files)
- `internal/provider/` — provider binding abstraction incl. `claudesession` (19 files)
- `internal/runtime/` — durable repo service (9 files)
- `internal/rlog/` — logging
- `internal/service/` — platform service integration (7 files)
- `internal/variant/` — Ralph persona profiles (17 files)
- `internal/voice/` — Ralph flavor text
- `internal/workspace/` — mirrors and worktrees (9 files)
- `internal/xdg/` — machine-local state paths
- `tests/integration/` — integration tests

Secondary:

- `.github/workflows/` — CI pipelines (7 workflows)
- `scripts/ci/` — CI helper scripts
- `docs/` — Sphinx docs, API reference (gomarkdoc), guides, runbooks
- `.radioactive-ralph/plans/` — plan state discipline
- `reference/` — reference implementation sources/tests (context only, lighter review)
- `site/` — marketing/docs site (context only)

## Flags

- Security Focus: no
- Performance Critical: no
- Strict Mode: no
- Framework: auto-detected — Go 1.x CLI/daemon, SQLite (plandag/db), Unix socket IPC, Sphinx docs, GitHub Actions CI

## Review Phases

1. Code Quality & Architecture
2. Security & Performance
3. Testing & Documentation
4. Best Practices & Standards
5. Consolidated Report
