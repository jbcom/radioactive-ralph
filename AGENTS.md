---
title: AGENTS.md — radioactive-ralph
updated: 2026-04-10
status: current
domain: technical
---

# Extended Agent Protocols — radioactive-ralph

## Architecture

Two-layer design:
1. `/autoloop` Claude Code skill — in-session orchestration
2. `radioactive-ralph` Python daemon — external persistence, survives context resets

The daemon spawns `claude` CLI subprocesses with `--dangerously-skip-permissions` and `--print`. Each subprocess is a stateless agent; the daemon holds all state.

## State

State file: `~/.radioactive-ralph/state.json` (default) or path from config.
Schema: `OrchestratorState` in `models.py`.
Never store state in `.claude/` directories — they trigger security firewalls.

## Work priority

| Priority | Enum value | Examples |
|----------|-----------|---------|
| 1 — CI_FAILURE | Highest | Fix broken CI |
| 2 — PR_FIXES | | Address review feedback |
| 3 — DOC_SWEEP | | Create missing CLAUDE.md etc. |
| 4 — MISSING_FILES | | Missing CHANGELOG, STANDARDS |
| 5 — STATE_NEXT | | Items from docs/STATE.md ## Next |
| 6 — DESIGN_FEATURE | | Items from docs/DESIGN.md ## Features |
| 7 — POLISH | Lowest | Code quality, refactors |

## Model selection

| Task | Model |
|------|-------|
| DOC_SWEEP, MISSING_FILES | `claude-haiku-4-5-20251001` |
| Feature work, bug fixes, PR review | `claude-sonnet-4-6` |
| DESIGN_FEATURE, security, architecture | `claude-opus-4-6` |

## Testing patterns

- Use `pytest-mock` (`mocker` fixture) — never `unittest.mock`
- Mark async tests with `@pytest.mark.asyncio`
- `tmp_path` for filesystem tests, `tmp_repo` fixture for repo simulation
- Run: `hatch run test`

## Adding a new command

1. Add a Click command to `cli.py`
2. Add the underlying logic to the appropriate module
3. Add a test in `tests/`
4. Update `docs/STATE.md`

## PR workflow

- Work on branches, open PRs, merge via GitHub
- All PRs need tests and passing CI before merge
- Use squash merge
