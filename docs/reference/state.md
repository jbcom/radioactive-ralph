---
title: State
updated: 2026-04-10
status: current
domain: context
---

# State — radioactive-ralph

## What's done

- Core Pydantic models (`models.py`) — PR status, work priority, orchestrator state
- PR manager (`pr_manager.py`) — classify, merge, and scan pull requests across repos
- Internal reviewer (`reviewer.py`) — Anthropic-powered review with tiered models
- Work discovery (`work_discovery.py`) — missing files, `docs/STATE.md`, and `docs/DESIGN.md` parsing
- Agent runner (`agent_runner.py`) — `claude` CLI subprocess orchestration
- State persistence (`state.py`) — JSON roundtrip, deduplication, pruning
- Config loader (`config.py`) — TOML + env layering
- Click CLI (`cli.py`) — `run`, `dashboard`, `status`, `discover`, `pr`, `stop`, `install-skill`
- Test suite — models, state, work discovery, PR manager
- Branded Shibuya docs with docs-native IA under `docs/getting-started`, `docs/guides`, `docs/variants`, and `docs/reference`
- GitHub Actions split into CI validation, `main` docs publishing, tag-based PyPI release, release asset refresh, and automerge

## Next

- Record and commit `assets/demo.gif`
- Design and upload `assets/social-preview.png`
- Produce per-variant SVG icons in `assets/variants/`
- Build the architecture SVG called for in `assets/ASSETS.md`
- Add integration coverage for the long-running daemon loop

## Known issues

- The demo GIF and social preview are still intentionally missing assets
- Some orchestrator follow-up work is still documented in this file rather than promoted into issues
- Long-running integration scenarios remain manual for now

## Active decisions

- Import package name: `radioactive_ralph` (PyPI name: `radioactive-ralph`)
- State location: `~/.radioactive-ralph/state.json` — never `.claude/`
- Default docs domain: `https://jonbogaty.com/radioactive-ralph/`
- Docs source model: root README for GitHub/PyPI, `skills/*/README.md` for skill canon, docs pages for curation and navigation
