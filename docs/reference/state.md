---
title: State
lastUpdated: 2026-07-16
---

# State

This page tracks what the runtime actually does today.

## What is live today

- Go CLI under `cmd/radioactive_ralph/`: `--supervisor`, dumb client mode,
  `--init`, `plan {import,ls}`, `doctor`, `service {install,uninstall,status}`
- the supervisor: pty ownership per agent (`internal/agent`), discovery
  socket + PID-lock single-instance + stale-socket reclaim
  (`internal/supervisor`), the reaper
- one user-level SQLite database (`internal/store`) holding project
  fingerprints, config, plans/tasks, spend accounting, and A2A
  evidence/messages
- config virtual layers (`internal/vconfig`): USER/PROJECTS composition,
  change-vs-override handling, conflict diffing, validation
- project identity via accumulated fingerprints (`store.Fingerprints`)
- plan markdown parsing + heuristic decomposition + validation
  (`internal/plan`)
- planning genesis: multi-agent juxtaposition/challenge refinement from a
  vague prompt to a plan document, plus TUI/`$EDITOR` review
  (`internal/genesis`)
- the orchestrator: dispatch, spend-cap admission, orchestrator-verified
  completion against acceptance criteria (`internal/orch`)
- A2A vocabulary adoption over `a2aproject/a2a-go` core types
  (`internal/a2a`)
- shipped provider bindings for `claude`, `codex`, `opencode`
  (`internal/provider`), plus a declarative config-only binding path for
  compatible CLI framings
- agent detection/classification (`internal/agentdetect`)
- native service-manager integration for launchd, systemd-user, and
  Windows SCM (`internal/service`)
- the read-only Bubble Tea TUI with macro/meso/micro drill-down
  (`internal/tui`)
- repo-root Sphinx docs and a generated Go API reference under
  `docs/api/`

## What changed from the earlier design

The live contract no longer includes:

- variants/personas (blue/green/savage/fixit/professor/old-man/
  world-breaker/etc.) — there is one mutating Ralph
- a durable **repo-scoped** service, a per-repo plan DAG database, or a
  committed `.radioactive-ralph/` config directory — replaced by the one
  supervisor and the one user-level database
- the socket-backed "cockpit"/"attach" framing — the client is a
  read-only view, not a second runtime to attach to
- `kong` for CLI parsing — replaced by `cobra` + `viper`
- confirmation-gate-per-variant / spend-cap-as-variant-floor — completion
  verification and spend caps are now orchestrator-level, not
  persona-level

Those concepts may still appear in the archived design record under
`docs/superpowers/`, which is intentionally preserved as history, not
live documentation.

## Remaining work

- richer TUI navigation and filtering ergonomics
- broader native-host smoke testing, especially on real Windows machines
- continued usage-frame parsing for `codex`/`opencode` so spend
  accounting covers every provider, not just `claude`

## What is intentionally true now

- Ralph is one binary, one supervisor, one mutating agent.
- The supervisor is the durable authority; the client is a read-only view.
- Providers are bindings, not the identity of the product.
- Completion is verified by the orchestrator, never asserted by a worker.
