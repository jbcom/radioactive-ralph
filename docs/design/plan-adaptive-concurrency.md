---
title: Plan-adaptive concurrency
description: How the dependency graph, leaf groups, and output reservations decide what may run at once.
---

# Plan-adaptive concurrency

A plan says what the work is. What may run **at the same time** is derived
from three separate questions, and keeping them separate is what makes the
answer correct:

1. **Readiness** — are this task's predecessors done?
2. **Grouping** — which tasks may one provider take in a single turn?
3. **Admission** — is it safe to start this task *right now*?

Collapsing any two of them produces a plausible-looking scheduler that runs
the wrong things concurrently.

## Readiness comes from the graph

Every plan imports as a set of tasks plus `task_deps` edges. A plan with no
`ralph-task` annotations gets edges derived from document order, so **a
linear plan is the degenerate case of a DAG**, not a separate code path.

Readiness is then one query: a task is ready when no edge points at an
unfinished predecessor. Dispatch walks that rather than re-parsing the
markdown and recomputing positions.

The difference is not academic. Position-based decomposition could only ever
surface **one leaf group per pass** — it walked groups in document order and
stopped at the first with unfinished work. A plan that fans into two
independent branches therefore serialized them for no reason, even though
the edges had already proven both were ready.

## Grouping decides what fan-out may take

Some providers can fan out internally: one invocation manages its own
sub-agents. When that happens, Ralph hands the provider a **whole leaf
group** — one heading, one resolved binding, one turn — rather than spawning
one worker per step.

That makes the *leaf group* the unit of fan-out, which is why every ready
task carries its persisted `group_path`. Deciding to fan out because
"several tasks are ready" would be wrong: in a DAG, tasks from unrelated
groups become ready simultaneously as the normal case, and handing one
provider two tasks from different groups means two headings and possibly two
bindings collapsed into one turn.

So the ready set is **partitioned by leaf group**, and fan-out applies only
within one partition.

### Tasks and workers are counted separately

A fan-out partition is N tasks on **one** worker. `maxParallel` bounds
workers, so charging it N would let a two-task fan-out consume a
`maxParallel=2` pipeline outright while a semaphore slot sat free and an
unrelated ready branch waited for the next tick.

## Admission is not readiness

Two tasks that write the same file are legitimately ready at the same
instant — neither consumes the other's result. An edge between them would be
a lie, and would serialize them *permanently* rather than only while one is
running.

So a declared exclusive output takes a **reservation** for the duration of a
run, checked inside the claim transaction:

- scoped to the **project**, not the plan, because a reservation protects a
  path in the project checkout and the supervisor dispatches every active
  plan concurrently — but not wider, since separate projects are separate
  checkouts where the same relative path is a different file;
- paths are **canonicalized**, so `build/out.txt` and `build/./out.txt`
  cannot each take a reservation on the same file;
- held only while the writer is **running**, so a finished holder never
  blocks its peer.

A reserved task still appears in the ready set. It *is* ready; it simply
cannot be claimed yet. Hiding it would conflate the two questions and make a
temporarily-unclaimable task indistinguishable from one whose predecessors
have not finished.

The two claim paths differ deliberately: a **named** claim refuses with
`ErrOutputReserved` because the caller asked for that specific task, while
the **unnamed** claim skips to the next candidate, since a blocked task must
not hide the other ready work behind it.

## What this does not decide

Concurrency limits are an admission budget, not a guarantee of parallelism.
A ready, unreserved, ungated task still waits for a dispatch slot, a
resolved binding, and any spend cap on its provider. Those are separate
gates with separate reasons, and each reports its own refusal rather than
silently returning "nothing ready".
