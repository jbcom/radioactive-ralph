---
title: CLAUDE.md — radioactive-ralph
updated: 2026-04-10
status: current
---

# radioactive-ralph — Agent Entry Point

Autonomous continuous development orchestrator. External Python daemon that drives Claude Code across a portfolio of GitHub repos.

## Quick orientation

```
src/radioactive_ralph/
├── cli.py          # Click CLI — entry point: ralph run/status/discover/pr/stop
├── orchestrator.py # Main async daemon loop
├── pr_manager.py   # gh CLI wrapper — classify, merge, sync
├── reviewer.py     # Internal code review via Anthropic API
├── work_discovery.py # Assess repos, read STATE.md, rank tasks
├── agent_runner.py # Spawn claude CLI subprocesses
├── state.py        # Durable JSON state read/write
├── models.py       # All Pydantic models
└── config.py       # TOML config loader
```

## Commands

```bash
uv sync --all-extras        # Install deps
hatch run test              # Run tests
hatch run lint:check        # Lint
hatch run lint:fmt          # Format
hatch run type-check        # mypy
hatch run docs              # Build Sphinx docs

ralph run                   # Start daemon
ralph status                # Show state
uvx radioactive-ralph run   # Run without installing
```

## Critical rules

- **No .claude/ for state** — state lives in `~/.radioactive-ralph/` or `docs/tasks/` in repos
- **SSH remotes only** — always `git@github.com:` never `https://`
- **Conventional commits** — `feat:`, `fix:`, `chore:`, `docs:`, etc.
- **300 LOC max per file** — enforced by ruff
- **pytest-mock over unittest.mock** — always use `mocker` fixture
