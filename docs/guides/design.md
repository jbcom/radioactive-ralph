---
title: Design
lastUpdated: 2026-04-14
---

# Design — radioactive-ralph

## Vision

AI-driven development should be **continuous and autonomous, not session-based**.
Today: open Claude, give it a task, wait for it to finish, tell it what to do
next. Tomorrow: open Claude (or don't), and it's already working.

radioactive-ralph is the bridge. A per-repo Python daemon that keeps a fleet
of Claude subprocesses alive, focused, and productive across days of work.

## What radioactive-ralph IS

- A **per-repo meta-orchestrator** that owns N managed `claude -p`
  subprocesses per session-variant
- A **persistent brain** that survives context resets via
  `claude -p --resume <session-id>`
- A **work discovery engine** that always finds the next valuable thing
- A **model-tiering layer** — haiku for bulk, sonnet for features, opus
  for architecture, per variant policy
- A **ten-variant personality matrix** — each variant a distinct
  behavior profile (parallelism, commit cadence, termination, tool
  allowlist, voice)

## What radioactive-ralph IS NOT

- An MCP server acting as a live bridge between an outer Claude session
  and the daemon. Confirmed impossible in Claude Code 2026 — interactive
  sessions have no IPC channel.
- A replacement for human judgment on vision and direction
- A way to merge unreviewed, untested code
- A vendor lock-in — uses `claude` CLI and `gh` CLI, both open
- A general-purpose task runner, tmux replacement, or SaaS orchestrator

## User experience (target — M2+)

The commands below describe the **target** UX post-rewrite. In M1 (the current
branch), only `radioactive_ralph status` and `radioactive_ralph doctor` are implemented; `radioactive_ralph init`,
`radioactive_ralph run`, `radioactive_ralph attach`, and `radioactive_ralph stop` land in M2 along with the real
daemon. See [state](../reference/state.md) for live implementation status.

```bash
# One-time per repo (M2)
ralph init

# Invoke directly from the terminal (M2)
ralph run --variant green --detach
ralph attach --variant green         # stream events

# Or invoke from inside a Claude session (M3 — skills become thin entry points)
/green-ralph
# → Ralphspeak pre-flight, background launch, hand-off
# → "Ralph is playing with his friends. Use the CLI to check on him."
```

Operator walks away. Comes back to:

```bash
gh pr list                           # PRs open across repos (always works)
ralph status --all                   # aggregate view of every variant (M2)
```

## Core principles

### Never halt
There is always more to do — open PRs, missing docs, STATE.md items,
features to build. If the queue is empty, run discovery again. The daemon
outlives context resets.

### Human engagement only for vision
Architecture decisions, product direction, new initiatives — human's
domain. Everything else: autonomous. The variants encode ten different
shapes of "autonomous."

### Cost efficiency
Haiku handles 80% of the work at 10% of the cost. Opus for <5% of tasks
where it genuinely matters. Variants declare their model tiering; the
supervisor dispatches based on the current stage of each task.

### External persistence
Context windows reset. Daemons don't. The daemon is the source of truth.
State lives in an append-only SQLite event log in XDG state dir — the
daemon replays it on every restart.

### Auditable
Every action creates a git commit or a PR. Nothing happens in the dark.
Event log is queryable after the fact.

### Safety by default
Destructive variants (old-man, world-breaker) work in isolated mirrors,
never touching the operator's working tree. Safety floors cannot be
weakened by a single config toggle.

## Workspace model

Rather than worktrees off the operator's repo (which fails if the operator
is already in a worktree, and pollutes their branch namespace), Ralph
mirrors the repo into XDG state and does all work there:

```text
~/src/myproject/                    ← operator works here, untouched by Ralph
├── .git/
└── .radioactive-ralph/
    ├── config.toml                 ← committed: variant policy
    └── local.toml                  ← gitignored

$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/
├── mirror.git/                     ← bare clone of operator's repo
├── worktrees/
│   ├── green-1/ ... green-6/
│   └── grey-current/
├── state.db
└── sessions/
```

Operator commits locally. Ralph's mirror fetches from `local` remote on
cadence, sees the commits, picks up work. Ralph opens PRs by pushing to
`origin` from its worktrees. Operator reviews on GitHub; merges through
the normal flow. Clean decoupling with zero shared mutable state.

## Variant personalities

Ten variants, each a distinct operating mode:

- **green** — The Classic. 6-worktree parallel, infinite loop, sensible tiering.
- **grey** — Mechanical sweep. 1-worktree, haiku only, single PR, done.
- **red** — Incident response. 8-worktree fan-out, single cycle.
- **blue** — Read-only observer. No writes, shared-repo isolation.
- **professor** — Plan → execute → reflect. Opus plans, sonnet executes.
- **fixit** — ROI-scored N-cycle bursts. Small PRs, bounded diffs.
- **immortal** — Multi-day resilient loop. Sonnet only, journaled state.
- **savage** — Max throughput. 10-worktree parallel, +1 tier, zero sleep. Gated.
- **old-man** — Forceful imposition. Force-resets, history rewrites. Hard-gated.
- **world-breaker** — All-opus everywhere. For catastrophes. Hardest gate.

See [variants index](../variants/index.md) for the full matrix.

## Non-goals

- GUI / web dashboard (terminal + `gh pr list` is sufficient)
- Multi-operator coordination (single-operator tool)
- Replacing CI/CD (augments, doesn't replace)
- Non-git workspaces, non-Anthropic LLMs, hosted/SaaS mode
