---
title: State
updated: 2026-04-10
status: current
domain: context
---

# State — radioactive-ralph

## What's done

- Core Pydantic models (`models.py`) — PRStatus, WorkPriority, OrchestratorState
- PR manager (`pr_manager.py`) — classify, merge, scan_all_repos
- Internal reviewer (`reviewer.py`) — Anthropic API, haiku/sonnet tiered
- Work discovery (`work_discovery.py`) — missing files, STATE.md, DESIGN.md
- Agent runner (`agent_runner.py`) — claude CLI subprocess, model selection
- State persistence (`state.py`) — JSON roundtrip, dedup, prune
- Config loader (`config.py`) — TOML with sensible defaults
- Click CLI (`cli.py`) — run, status, discover, pr list/merge, stop
- Test suite — models, state, work_discovery, pr_manager (pytest-mock)
- Sphinx docs with RTD theme, mascot hero image
- GitHub Actions: ci.yml, release-please.yml, release.yml (OIDC PyPI + Pages)
- release-please config + manifest
- dependabot weekly updates

## Next

- Fix any mypy errors surfaced by `hatch run hatch-test:type-check`
- Push initial commit to git@github.com:jbcom/radioactive-ralph.git
- Enable GitHub Pages via gh API
- Open first PR and let CI run
- Address any CI failures
- Resume kings-road and infinite-headaches doc sweeps (hit API limits)
- Launch batch 2 second half (9 repos)
- Merge batch 1 and completed batch 2 PRs as CI passes

## Known issues

- kings-road and infinite-headaches doc sweep agents hit API rate limits before creating PRs — need re-run
- radioactive-ralph has no integration tests yet — add when daemon loop is battle-tested
- `orchestrator.py` — agent_runner import and some Pyright warnings may need cleanup

## Active decisions

- Import package name: `radioactive_ralph` (PyPI: `radioactive-ralph`) — standard Python convention
- State location: `~/.radioactive-ralph/state.json` — never `.claude/`
- Model default: sonnet for most work, haiku for bulk doc sweeps
