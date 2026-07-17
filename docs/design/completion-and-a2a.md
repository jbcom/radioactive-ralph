---
title: Orchestrator-verified completion and A2A
description: Why a worker never marks its own work done, and the A2A vocabulary that carries evidence to the orchestrator.
---

# Orchestrator-verified completion and A2A

## The trust model

A step is not done because a worker says so, and especially not because
the worker's process terminated — termination may be a crash. A worker
submits **evidence** of completion (what it ran, exit codes, output,
diff). Only the orchestrator (`internal/orch.VerifyAndComplete`)
transitions a task to a terminal `done` state, after checking that
evidence against the step's actual acceptance criteria:

- if the step declares a `Command`, it must exit 0 when re-run
- if the step declares a `FileExists` path, it must exist
- absent either, the step is judgment-only: non-empty evidence output is
  the best available signal

This is the correctness backbone. Nothing downstream ever depends on a
worker's self-assessment.

## A2A vocabulary

Worker↔orchestrator coordination uses the official `a2aproject/a2a-go`
SDK's core types (`internal/a2a`) for vocabulary — `a2a.TaskState`,
`a2a.Message` — without adopting its heavier server packages. Only the
stdlib-only core-types package is imported; `a2asrv`/`a2agrpc`/`a2apb`
are not pulled in because there is no network-facing A2A surface today.

Task states used:

| State | Meaning |
|-------|---------|
| `Submitted` | Claimed and dispatched; worker hasn't reported yet |
| `Working` | Worker actively executing |
| `InputRequired` | The never-block signal — the orchestrator handles it (re-dispatch with an answer, or kill-and-reclaim), never by waiting on a blocked pty |
| `Completed` | Orchestrator-verified only; never set from a worker's self-report or from process termination |

A worker submitting evidence via an `a2a.Message` is a **proposal** for
the orchestrator to verify — never a self-assertion of completion.

## Durability

Evidence and task-state transitions live in the one user-level SQLite
database (the `a2a_messages` table plus the plan/task tables) — the plan DAG's existing
task store is the durable authority, not a second parallel store. The
message table is a log of evidence, not an alternate source of truth.

## Squads and native fan-out

For a parallelizable step-group, the orchestrator may fan out multiple
worker processes with path/worktree isolation, or — when the bound
provider's capability record declares `NativeFanout` (see [Provider
contract](./provider-contract.md)) — delegate the whole step-group to
one invocation of that provider instead of spawning N Ralph-managed
workers. Either way, cost and progress propagate up to the macro TUI
through the same store-backed accounting.
