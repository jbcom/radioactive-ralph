---
title: Safety floors & shell trust
description: Non-negotiable guardrails the supervisor enforces, and how to opt into destructive operations.
---

Ralph manages autonomous Claude sessions that run for hours or days
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
non-merged branches — the supervisor raises a gate that blocks the
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
freezes autonomous sessions indefinitely.

### 5. Conventional commits

The supervisor's system-prompt bias includes a conventional-commits
rule. Variants that don't honor it get their commits rejected at
the commit-message hook level.

### 6. `cmd.ExtraFiles` lifeline pipe

Every spawned variant subprocess holds a read handle on FD 3 tied to
a pipe the supervisor owns. When the supervisor dies for any reason
(clean exit, crash, SIGKILL, OOM), the child reads EOF and
self-terminates within ~3 seconds. See `internal/variantpool/pool.go`
and `internal/proclife` for the per-platform belt-and-suspenders.

**Why:** without this, a supervisor that crashes leaves orphan Claude
subprocesses burning API tokens indefinitely.

## `ShellExplicitlyTrusted`

A single operator-set flag in `.radioactive-ralph/local.toml`
(gitignored) that unlocks the confirmation gates for variants the
operator has already vetted. It is **intentionally** machine-local:
it cannot be committed to the repo or propagated across machines.

```toml
# .radioactive-ralph/local.toml
[shell]
explicitly_trusted = true

[shell.variants]
# Per-variant grants — a blanket trust flag isn't enough; the variant
# profile must also declare shell_trust_eligible = true.
green = true
```

When both are present, the supervisor skips the shell-confirmation
gate for that variant. All other floors (isolation, plans-first,
SSH, lifeline) still apply.

## Auditing

Every time a safety floor triggers — whether it passes or blocks —
the supervisor writes an event into the plan DAG's `task_events`
table. `radioactive_ralph plan history --events floor` surfaces them.
