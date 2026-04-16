---
title: Safety floors & shell trust
description: Non-negotiable guardrails the runtime enforces, and how to opt into destructive operations.
---

Ralph manages long-running provider sessions that run for hours or days
without operator attention. **Safety floors** are the set of
non-negotiable guardrails that keep those sessions from damaging the
repo, the operator's machine, or the broader state of the world.
This page documents each floor and the narrow, deliberate ways to
opt into more aggressive behavior.

## The floors

### 1. Mirror-based isolation for destructive variants

Variants whose profile declares `object_store = full` (in
`internal/variant`) **must** be spawned inside a worktree backed by a
`git clone --mirror`, not the operator's repo. This is enforced in
`internal/workspace` at spawn time; single-flag overrides are
rejected.

**Why:** a destructive variant that rebases history or force-pushes
can corrupt the operator's working copy. Mirror-based isolation
ensures every destructive op runs against a disposable clone.

### 2. Confirmation gates on destructive operations

Before certain operations — force-push, `git reset --hard`, deleting
non-merged branches — the runtime raises a gate that blocks the
session until the operator confirms. Gates are defined per-variant
in the variant profile.

**Opt-out:** set `ShellExplicitlyTrusted = true` in the variant
config *and* supply the matching variant profile tag. Both must be
present; one-flag overrides fail closed.

### 3. Plans-first discipline

Every variant except `fixit` refuses to boot without an active plan.
This prevents "let me just start working" sessions that drift into
unscoped churn.

**Opt-out:** run `fixit --advise` to produce a plan first. There is
no way to run a non-fixit variant without one.

### 4. SSH-only git remotes

Ralph rewrites `https://` git remotes to their `git@` form when
attaching a worktree, and refuses to operate on a worktree whose
origin is still HTTPS. Enforced in `internal/workspace`.

**Why:** HTTPS remotes default to prompting for credentials, which
freezes long-running sessions indefinitely.

### 5. Conventional commits

The runtime's system-prompt bias includes a conventional-commits
rule. Variants that don't honor it get their commits rejected at
the commit-message hook level.

### 6. Durable runtime ownership

The durable repo service owns long-running work, worktree state, and provider
subprocess execution. Attached runs are bounded specifically so the operator is
not depending on orphaned background state.

**Why:** the runtime should be the only authority over durable execution.

## Confirmation flow

The live runtime does not use a committed shell-trust block. Destructive
variants are gated by explicit run-time confirmations and, where applicable,
spend caps declared either by CLI flag or variant config. Operator intervention
flows through:

- `radioactive_ralph plan approvals`
- `radioactive_ralph plan approve <plan> <task>`
- `radioactive_ralph plan blocked`
- `radioactive_ralph plan requeue <plan> <task>`
- `radioactive_ralph plan retry <plan> <task>`
- `radioactive_ralph plan handoff <plan> <task> <variant>`

## Auditing

Every time a safety floor triggers — whether it passes or blocks —
the runtime writes an event into the plan DAG's `task_events`
table. `radioactive_ralph plan history <plan> <task>` surfaces the
per-task floor events.
