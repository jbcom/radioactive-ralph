---
title: Architecture
updated: 2026-04-10
status: current
domain: technical
---

# Architecture — radioactive-ralph

## Two-mode design

radioactive-ralph ships in two deployment modes, backed by the same work-discovery,
PR-classification, and forge-interaction code:

```
┌─────────────────────────────────────────────────────────────┐
│  Mode 1: Claude Code plugin (10 Ralph variants)              │
│  /green-ralph, /red-ralph, /professor-ralph, …               │
│  Runs inside an active Claude Code session via the Agent     │
│  tool. Uses the user's existing auth — no separate API key.  │
│  Survives: the length of the Claude Code session.            │
│  Install: claude plugin install radioactive-ralph            │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  Mode 2: External daemon (this Python package)               │
│  `ralph run` — Python asyncio daemon, spawns `claude`        │
│  CLI subprocesses per work item. Lives *outside* any Claude  │
│  session.                                                    │
│  Survives: context resets, PR merges, process restarts,      │
│  rate limits, network blips (via immortal-ralph mode).       │
│  State: ~/.radioactive-ralph/state.json                      │
│  Config: pydantic-settings layered over TOML + env vars.     │
│  Install: pip install radioactive-ralph (or uvx ...)         │
└─────────────────────────────────────────────────────────────┘
```

Both modes share the same skill family and the same Ralph Wiggum personality
module (`ralph_says.py`) — the daemon logs in Ralph's voice, the plugin prompts
the in-session agent to behave like the chosen variant.

## Module map

| Module | Responsibility |
|--------|---------------|
| `cli.py` | Click entry point — `ralph run/status/discover/pr/stop` |
| `orchestrator.py` | Main async daemon loop — 8-phase cycle |
| `pr_manager.py` | `gh` CLI wrapper — classify, merge, sync |
| `reviewer.py` | Anthropic API code review — haiku/sonnet tiered |
| `work_discovery.py` | Repo assessment — missing files, STATE.md, DESIGN.md |
| `agent_runner.py` | Spawn `claude` CLI subprocesses |
| `state.py` | JSON state read/write, dedup, prune |
| `models.py` | All Pydantic models |
| `config.py` | TOML config loader |

## Orchestrator cycle (8 phases)

```
ORIENT → DRAIN_MERGE_QUEUE → INTERNAL_REVIEW → ADDRESS_FEEDBACK
      → DISCOVER_WORK → SPAWN_AGENTS → HANDLE_COMPLETIONS → SLEEP(30s)
```

1. **ORIENT** — load state, check signal handlers
2. **DRAIN_MERGE_QUEUE** — merge all MERGE_READY + CI-passed PRs, sync local
3. **INTERNAL_REVIEW** — run Anthropic review on NEEDS_REVIEW PRs
4. **ADDRESS_FEEDBACK** — spawn agents to fix HIGH/ERROR findings
5. **DISCOVER_WORK** — scan repos for missing files, STATE.md next items
6. **SPAWN_AGENTS** — launch `claude` subprocesses up to `max_parallel_agents`
7. **HANDLE_COMPLETIONS** — collect results, extract PR URLs, update state
8. **SLEEP** — 30s, then loop

## PR status lifecycle

```
DRAFT → IN_PROGRESS → NEEDS_REVIEW → [INTERNAL_REVIEW] → NEEDS_FIXES
                                   → MERGE_READY → merged
                   → CI_FAILING → (fix) → NEEDS_REVIEW
                   → STALE → (prune)
```

## Work priority

Lower number = higher priority. CI failures always win.

| Priority | Value | Trigger |
|----------|-------|---------|
| CI_FAILURE | 1 | Any CI failure in any repo |
| PR_FIXES | 2 | Review findings with ERROR severity |
| DOC_SWEEP | 3 | Missing standard doc files |
| MISSING_FILES | 4 | Missing CLAUDE.md, AGENTS.md, etc. |
| STATE_NEXT | 5 | `## Next` section in docs/STATE.md |
| DESIGN_FEATURE | 6 | `## Features` section in docs/DESIGN.md |
| POLISH | 7 | Quality improvements when nothing else |

## Model tiering

| Task | Model | Reason |
|------|-------|--------|
| DOC_SWEEP, MISSING_FILES | haiku | Bulk mechanical work |
| Feature work, PR review, bug fixes | sonnet | General purpose |
| DESIGN_FEATURE, security, architecture | opus | Deep reasoning required |

## State persistence

State is a single `OrchestratorState` JSON file. Location (in priority order):
1. `--state-path` CLI flag
2. `state_path` in config TOML
3. `~/.radioactive-ralph/state.json` (default)

Never use `.claude/` — triggers security firewalls in some environments.

## `uvx` compatibility

`[project.dependencies]` contains only runtime deps (`anthropic`, `click`, `pydantic`, `rich`).
`uvx radioactive-ralph run` installs and runs in an isolated environment in seconds.
