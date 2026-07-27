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

## Ordering is total, not incidental

Ready tasks are claimed in `sequence_ordinal` order with `created_at` as the
tie-break. Both are recorded at import, so two runs of one plan visit ready
tasks in the same order rather than in whatever order the storage engine
happens to return rows.

Dependency edges make that ordering *correct*; the deterministic tie-break
makes it *repeatable* when the edges leave a genuine choice.

## Cycles are refused, not detected at runtime

`AddDep` runs a reachability check before inserting an edge and refuses one
that would close a cycle. A plan that cannot make progress is rejected at
import rather than discovered as a run that quietly stops advancing.

## A claim is atomic and owner-guarded

Claiming transitions a task to `running` inside a transaction, and the result
is checked with `RowsAffected` — so two workers can never both believe they
own one task. A guard that "found the task claimable" and then wrote without
re-checking would be exactly the race this closes.

Completion and failure carry the same guard: the write requires
`claimed_by_session` to still match the reporting session, so a late report
from a worker the reaper already reclaimed cannot overwrite the current
owner's result. That is `ErrTaskNotOwnedRunning`, and it is treated as benign
rather than fatal — the stale worker simply lost, and the current owner's
attempt stands.

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
  provider is a separate process writing by pathname minutes later. Ralph's
  guarantee is that an escaping output is *detected* at completion, not that
  it cannot happen.
