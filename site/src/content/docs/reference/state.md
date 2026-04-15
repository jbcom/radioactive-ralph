---
title: State
updated: 2026-04-14
status: current
domain: context
---

# State — radioactive-ralph

The project is mid-rewrite. This page tracks concrete status per component.
See [the PRD](../plans/2026-04-14-radioactive-ralph-rewrite.prq.md) for the
four-milestone plan.

## What's done

### M1 (this branch): marketplace + hygiene

- `.claude-plugin/marketplace.json` — marketplace renamed to `jbcom-plugins`,
  plugin renamed to `ralph`, `strict: false`, skills listed explicitly.
  Validates under `claude plugin validate .`.
- README install + command documentation corrected — no more claims of
  commands that don't exist, no more `claude --print` fiction.
- Dead code removed — `github_client.py` (the legacy class) deleted.
  Auth helpers moved to `forge/auth.py` where they belong.
- Broken implementations stubbed — `Orchestrator.run`,
  `Orchestrator.stop`, `agent_runner.run_parallel_agents` raise
  `NotImplementedError` with pointers to the PRD. The inner helpers
  (`_merge_ready`, `_review_pending`, `_should_discover`) are preserved
  as reusable building blocks for M2.
- CLI surface pruned — `radioactive_ralph status` + `radioactive_ralph doctor` implemented;
  `radioactive_ralph run` is a stub that exits 2 with the PRD pointer.
- Two broken tests fixed — `test_cli.py::test_main_verbose` had an empty
  `pass` body; `test_orchestrator.py::test_step_spawns_agents` passed
  `repo_name` to a Pydantic model where it's a computed property.
- Domain docs (`architecture.md`, `design.md`, `state.md`, `testing.md`)
  rewritten to the target architecture.

### Previously done (pre-rewrite, preserved)

- Core Pydantic models (`models.py`)
- PR manager (`pr_manager.py`) — classify, merge, scan
- Internal reviewer (`reviewer.py`) — Anthropic-powered with tiered models
- Work discovery (`work_discovery.py`) — missing-file heuristics
- State persistence (`state.py`) — JSON roundtrip, dedup, prune
- Config loader (`config.py`) — TOML + env layering
- Forge abstraction (`forge/`) — GitHub + Gitea + GitLab backends
- Test suite — 143 tests, covering models, state, forges, reviewer,
  PR manager, work discovery, dashboard, doctor, CLI, ralph_says
- Shibuya-themed docs site published at <https://jonbogaty.com/radioactive-ralph/>
- GitHub Actions split across CI validation, docs publishing, tag-based
  PyPI release, and automerge

## What's planned

### M2 — daemon skeleton + per-repo config + XDG workspace + session control

- `radioactive_ralph init` wizard creating `.radioactive-ralph/`
- `WorkspaceManager` dispatching across four isolation modes
- SQLite + sqlite-vec event log with WAL
- Unix socket IPC for `radioactive_ralph status / attach / enqueue / stop`
- `ClaudeSession` wrapping `claude -p --input-format stream-json`
- Multiplexer abstraction (tmux → screen → setsid fallback)
- `Orchestrator` rewrite as a per-repo supervisor

### M3 — ten variants + pre-flight + voice

- `Profile` dataclass; ten variant files (≤300 LOC each)
- Pre-flight wizard with shared question registry (CLI + skill)
- Voice template library per variant
- Safety floors with two-step override for destructive variants
- Auto-generated variants-matrix.md (CI drift check)
- Skills rewritten as thin entry points (<30 lines each)

### M4 — integration harness + doctor + release

- Integration test scenarios (grey-ralph end-to-end; session death
  recovery; pre-flight refusal; multiplexer fallback; LFS detection;
  hook preservation; shared-object corruption recovery)
- `radioactive_ralph doctor` rewrite with concrete remediation output
- Demo GIF recording the full flow
- Release 1.0.0 to PyPI

## Known issues (during rewrite)

- `radioactive_ralph run` is stubbed (exits 2). Real daemon lands in M2.
- Ten variants are still SKILL.md files; behavior is not yet in Python.
- Mirror-based workspace architecture is documented but not implemented.
- No `.radioactive-ralph/` directory is created anywhere yet.

## Active decisions

- Import package name: `radioactive_ralph` (PyPI name: `radioactive-ralph`)
- Config location: `.radioactive-ralph/config.toml` per repo (committed)
- State location: `$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/`
  (never `.claude/`, never the repo)
- Docs domain: <https://jonbogaty.com/radioactive-ralph/>
- Docs source model: README for GitHub/PyPI; skill files for skill canon;
  `docs/` pages for curated narrative and auto-generated matrices
