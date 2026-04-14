---
title: CLAUDE.md — radioactive-ralph
updated: 2026-04-14
status: current
---

# radioactive-ralph — Agent Entry Point

Autonomous continuous development orchestrator. Per-repo Python daemon that
keeps Claude subprocesses alive, focused, and productive across days of work.

**Currently mid-architectural-rewrite.** See
[`docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`](docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md)
for the four-milestone plan (M1: hygiene; M2: daemon skeleton; M3: variants;
M4: integration + release).

## Quick orientation

```text
src/radioactive_ralph/
├── cli.py          # Click CLI — `ralph status` / `doctor` implemented; `run` stubbed pending M2
├── orchestrator.py # Legacy loop stubbed; helpers preserved (_merge_ready, _review_pending, _should_discover)
├── agent_runner.py # Stubbed pending M2 rewrite (previous impl used non-existent `claude --message`)
├── pr_manager.py   # PR classification + merge, uses forge/ abstraction (NOT the gh CLI directly)
├── reviewer.py     # Internal code review via Anthropic API
├── work_discovery.py # Assess repos, read STATE.md, rank tasks
├── state.py        # Durable JSON state read/write
├── models.py       # All Pydantic models
├── config.py       # TOML config loader
├── dashboard.py    # `ralph status` rendering
├── doctor.py       # `ralph doctor` environment check
├── logging_setup.py
├── ralph_says.py   # Personality voice (currently 2 variants; expanded to 10 in M3)
├── git_client.py   # git subprocess wrappers
└── forge/          # GitHub/Gitea/GitLab REST API abstraction
    ├── __init__.py
    ├── base.py     # Protocol + ForgeInfo + ForgePR + PRCreateParams
    ├── auth.py     # GitHub token discovery (env → gh CLI)
    ├── github.py   # GitHubForge REST client (httpx)
    ├── gitea.py
    └── gitlab.py
```

## Commands

```bash
uv sync --all-extras                # Install deps
hatch test                          # Run tests
hatch fmt --check                   # Lint + format check
hatch run hatch-test:type-check     # mypy strict
hatch run docs:build                # Build Sphinx docs

# When hatch isn't available:
uv run --all-extras pytest
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run mypy src/

# Runtime (post-M2):
ralph init                          # per-repo setup wizard
ralph run --variant X [--detach]    # launch supervisor
ralph status [--variant X | --all]  # query Unix socket
ralph attach --variant X            # stream events
ralph stop [--variant X]            # graceful shutdown
ralph doctor                        # environment health
```

## What radioactive-ralph is NOT

- An MCP server acting as a live bridge between an outer Claude session and
  the daemon. Confirmed impossible in Claude Code 2026 — interactive sessions
  have no IPC channel for external user-message injection.
- A general-purpose task runner / tmux replacement / SaaS orchestrator.
- A replacement for human judgment on vision and direction.
- A multi-operator coordination tool (one operator per daemon).
- A non-git workspace tool.
- A multi-LLM-provider framework (Anthropic only).

## Critical rules

- **Config lives in-repo**: `.radioactive-ralph/config.toml` (committed) +
  `.radioactive-ralph/local.toml` (gitignored). Missing config = refuse to run.
- **State lives in XDG**: `$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/` via
  `platformdirs.user_state_dir`. Never `.claude/`, never in the repo tree.
- **SSH remotes only** — `git@github.com:`, never `https://`.
- **Conventional commits** — `feat:`, `fix:`, `chore:`, `docs:`, etc.
- **300 LOC max per file** — enforced by ruff.
- **pytest-mock over unittest.mock** — always use `mocker` fixture.
- **stream-json is the session protocol** — daemon spawns
  `claude -p --input-format stream-json --output-format stream-json` and
  pipes user messages to stdin. Never interactive mode for managed sessions.
- **Mirror-based workspaces** for `mirror-*` isolation variants — worktrees
  are created off a `git clone --mirror` in XDG, not off the operator's repo.
