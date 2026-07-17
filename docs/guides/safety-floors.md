---
title: Safety floors
description: The non-negotiable guardrails the supervisor enforces on every agent.
---

# Safety floors

Ralph runs agent CLIs for hours or days without operator attention. These
are the non-negotiable guardrails that keep those sessions from blocking
the system or running away on spend.

## 1. The never-block control invariant

An agent CLI must never block the system — no permission prompts, no
clarification waits, no interactive menus. The supervisor owns every
agent's pty directly (`creack/pty`, `internal/agent`) and runs a watchdog
per agent that classifies output as normal progress, a stall
(no-output-for-N), or an interactive prompt pattern. Any of those signals
triggers auto-resolve, deny, or **kill-and-reclaim** — the supervisor
never waits.

Kill is cheap because state is durable: the plan slice a worker was
executing lives in the one user-level database, so recovery is replaying
that slice to a fresh worker process, not resuming a fragile session.

## 2. Completion is orchestrator-verified

A step is not done because a worker says so, and especially not because
the worker's process terminated (termination may be a crash, not
success). A worker submits evidence of completion — what it ran, exit
codes, output, diff. Only the orchestrator (`internal/orch`) transitions
a task to `done`, by re-running the step's acceptance check (a command
that must exit 0, a file that must exist) or, absent a mechanical check,
requiring non-empty evidence output. This is the correctness backbone:
nothing downstream depends on a worker's self-assessment.

## 3. Spend caps

Providers can be configured with a spend cap. The orchestrator
accumulates each provider's reported cost per project
(`internal/orch/spend.go`) and refuses to dispatch further work on a
provider once its accumulated spend reaches the cap — other ready steps
on an uncapped or under-cap provider still dispatch normally. Cost
metering is populated from provider-reported usage frames where
available.

## 4. One user-level database, clean repos

All plan, project, config, and spend state lives in the one user-level
SQLite database under the XDG data root. Nothing is committed to a
repo — no config directory, no per-repo database — so there is nothing
in version control that can encode or leak execution state, and no
merge-conflict surface from concurrent runs.

## 5. Local-only providers

Shipped providers (`claude`, `codex`, `opencode`) each own their own
agent loop and tool execution locally; calling a hosted model for
inference is fine, but the CLI itself never delegates session control to
a cloud-hosted control surface. This keeps the pty-ownership and
never-block guarantees meaningful — a provider that handed control to a
remote service couldn't be watched or killed the same way.
