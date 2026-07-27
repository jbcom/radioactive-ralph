---
title: Deterministic execution
description: What Ralph guarantees is reproducible about a run, and what it deliberately does not.
---

# Deterministic execution

Ralph drives non-deterministic agents. The output of any single turn is not
reproducible and never will be. What *is* reproducible is everything around
it: which task ran, in what order, against which inputs, and on whose
authority a task was marked done.

Being precise about that boundary matters more than claiming determinism
Ralph cannot deliver. Each guarantee below names the mechanism that provides
it, so a reader can check the claim rather than trust it.

## The plan text is write-once

A plan stores its `source_markdown` at creation and nothing updates it. Every
later decision — decomposition, ids, edges — derives from that stored text
rather than from a file on disk that may have changed since.

This is why re-running a plan reasons about the same document the first run
saw, even if the operator has since edited the file it came from.

## Task identity is stable

Every task has a stable id: an explicit `id` from its `ralph-task`
annotation, or the positional `StepRef` id (`0.1`) when unannotated. One rule,
shared by import and dispatch.

The alternative is not hypothetical. When those two disagreed, importing a
step annotated `{"id": "build"}` created task `build`, and dispatch then
derived its own positional `0.0` and materialized a **second** task for the
same step — a duplicate node, with whichever one got claimed deciding whether
the run made sense.

## Ordering is total for sequential steps, and NOT yet total for parallel ones

Ready tasks are claimed in `sequence_ordinal` order with `created_at` as the
tie-break, so a sequential group is visited in author order rather than in
whatever order the storage engine returns rows.

That is not yet a *total* order. Graph import does not set
`sequence_ordinal` for parallel steps, and a whole plan is imported in one
transaction, so simultaneously ready roots and parallel siblings can share a
`created_at` — leaving the final tie to the storage engine. Dependency edges
still make the ordering *correct*; what is missing is a genuinely total
tie-break (a per-plan insertion ordinal) that would make the remaining
choices *repeatable*. Until that exists, treat parallel-sibling order as
unspecified rather than stable.

## Cycles are refused, not detected at runtime

`AddDep` runs a reachability check before inserting an edge and refuses one
that would close a cycle. A plan that cannot make progress is rejected at
import rather than discovered as a run that quietly stops advancing.

## A claim is atomic and owner-guarded

Claiming transitions a task to `running` inside a transaction, and the result
is checked with `RowsAffected` — so two workers can never both believe they
own one task. A guard that "found the task claimable" and then wrote without
re-checking would be exactly the race this closes.

Completion and failure carry the same guard at the STORE layer: `MarkDone` and
`MarkFailed` require `claimed_by_session` to match the session they are given,
and `ErrTaskNotOwnedRunning` is treated as benign rather than fatal — the
stale worker simply lost.

There is a live gap above that guard. `VerifyAndComplete` does not receive the
reporting worker's session; it reads `task.ClaimedBySession` at verification
time and passes THAT to `MarkDone`. So when worker A returns after the reaper
reclaimed its task and worker B claimed it, A's result is written under B's
session — the store guard is satisfied by a session that did not produce the
evidence, and B's attempt is overwritten rather than preserved. Closing it
means threading the reporting session from dispatch into verification so the
guard compares against the worker that actually ran.

## Completion is orchestrator-verified

A worker never marks its own work done. The orchestrator re-runs the task's
acceptance criteria — a command that must exit zero, a file that must exist,
or, absent either, a judgment over the submitted evidence — and only then
marks it done.

A worker's claim of success, or its process simply exiting cleanly, is never
sufficient on its own. See
[Orchestrator-verified completion and A2A](./completion-and-a2a.md).

Acceptance criteria are derived at **import** and stored on the task, not
recomputed at completion time. An empty `acceptance_json` selects
judgment-only verification, so a task whose criteria were dropped somewhere in
the pipeline would silently downgrade to "the worker produced some evidence"
— which is why the derivation happens once, at ingress, in the same
transaction as the task.

## What is NOT deterministic

Stated plainly, because a guarantee list read as exhaustive is worse than one
that names its own edges:

- **Agent output.** Two runs of one task produce different text. That is the
  premise, not a defect.
- **Wall-clock interleaving.** Which of several ready tasks finishes first
  depends on the providers, the machine, and the network.
- **Provider-side model behavior.** A model version can change under a
  binding. That is what calibration records exist to make *visible* — a
  measurement pinned to an exact command line — rather than to prevent.
- **Filesystem contents between admission and use.** Declared-path
  containment is best-effort validation, not a write-side boundary: the
  provider is a separate process writing by pathname minutes later. A task
  that declares `outputs` and then writes elsewhere is NOT currently detected
  — no completion-time check reads those declarations. Ralph validates the
  declared paths themselves at admission and nothing more, so `outputs` is a
  statement of intent rather than an enforced boundary in either direction.
