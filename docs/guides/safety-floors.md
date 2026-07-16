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
in the variant profile and are surfaced through the operator task
controls (`plan approvals`, `plan approve`, `plan blocked`,
`plan requeue`, `plan retry`, `plan handoff`).

Gate confirmation differs by execution path:

- **Attached (`radioactive_ralph run --variant X`)** — the operator passes
  the per-invocation flag (`--confirm-burn-budget`, `--confirm-no-mercy`,
  `--confirm-burn-everything`). The run refuses to start without it.
- **Durable (`radioactive_ralph service start`)** — the service is
  non-interactive, so authorization lives in the gitignored `local.toml`:

  ```toml
  # .radioactive-ralph/local.toml  (gitignored)
  confirm_durable_variants = ["savage"]   # savage | old-man | world-breaker
  ```

  Authorization lives in `local.toml` and **never** in committed
  `config.toml` — a pull request must not be able to flip a plan to a
  destructive variant and have the service run it. The durable scheduler
  refuses to dispatch a gated variant that is not listed (and a
  spend-cap-required variant with no cap), emitting a
  `worker.admission_refused` event with the reason; the task stays pending
  until the operator authorizes it.

The `ShellExplicitlyTrusted` field on the variant profile is reserved
for a future "opt-out" path, but the live runtime does not honor it —
destructive variants are gated exclusively by explicit run-time
confirmations and spend caps declared either by CLI flag or variant
config. There is no committed shell-trust block today. When a future
release wires `ShellExplicitlyTrusted` into the gate logic, both the
flag and the matching variant profile tag will be required; one-flag
overrides fail closed.

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

## Spend caps

Variants whose profile declares `RequireSpendCap` (savage, world-breaker)
must have a cap set — `--spend-cap-usd` on the attached path, or
`[variants.<name>] spend_cap_usd` in `config.toml` for the durable service.
The durable runtime accumulates the provider-reported cost per variant and
stops dispatching that variant once its running total reaches the cap,
recording `worker.spend` and `worker.spend_cap_exceeded` events. Cost
metering is populated by the `claude` runner today; other providers require
a cap value but are not yet cost-metered.

## Confirmation flow

Destructive variants are gated by explicit run-time confirmations
and, where applicable, spend caps declared either by CLI flag or
variant config. Operator intervention flows through:

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
