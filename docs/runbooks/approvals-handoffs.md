---
title: Approvals + handoffs
description: How to gate tasks for operator approval and hand tasks between variants.
---

The plan DAG supports two operator-driven workflows on top of the
normal claim-execute loop:

- **Approvals** — a task sits in `ready_pending_approval` until the
  operator releases it. Used for high-stakes work (destructive
  migrations, production rollouts, anything with a confirmation gate
  in the variant's safety floors).
- **Handoffs** — the operator re-routes a task from one variant to
  another without retrying. Used when the originally-assigned variant
  isn't the right fit.

## Approvals

### Mark a task for approval

A task enters the approval state when:

- fixit's plan-creation pipeline emits it with `status:
  ready_pending_approval` (happens automatically for variants whose
  `SafetyFloors.RequireOperatorApproval = true`)
- an operator requeues a blocked, failed, pending, or approval-gated
  task with approval still required:

```sh
radioactive_ralph plan requeue <plan> <task> --require-approval
radioactive_ralph plan handoff <plan> <task> <variant> --require-approval
```

### Approve a pending task

```sh
radioactive_ralph plan approvals                # list tasks awaiting approval
radioactive_ralph plan approve <plan> <task>    # release a single task
```

Approved tasks move back to `pending`; the ready query promotes them
when their dependencies are satisfied, and the next variant to poll
the DAG claims them normally.

### Deny / requeue

If you don't want to run a pending task at all:

```sh
radioactive_ralph plan requeue <plan> <task>    # send back to pending
radioactive_ralph plan fail <plan> <task>       # mark failed, don't retry
```

## Handoffs

### List blocked or failed tasks

```sh
radioactive_ralph plan blocked
radioactive_ralph plan tasks <plan> --status failed
```

### Hand a requeueable task to a different variant

```sh
radioactive_ralph plan handoff <plan> <task> <new-variant>
```

What this does:

1. Sets the task's next `variant_hint`
2. Clears `assigned_variant`, `claimed_by_session`, and `claimed_by_variant_id`
3. Moves the task back to `pending`, or `ready_pending_approval` if
   `--require-approval` is set
4. Records a durable `requeued` event with the handoff payload

The new variant claims it on the next poll.

### Handoff policies

Handoff itself does not bypass a variant's normal confirmation gates.
For example, `savage`, `old-man`, and `world-breaker` still require
their execution-time confirmation flags when the hinted variant runs.

## Retry budget

Every failed task keeps a retry count. The operator can manually
retry:

```sh
radioactive_ralph plan retry <plan> <task>
```

Retries increment the task's retry counter and move the task back to
`pending`. If the task was not blocked or failed, the command refuses
the transition.

## History

Every approval, handoff, retry, and failure is an event in
`task_events` (durable, per-plan). To see the history for one task:

```sh
radioactive_ralph plan history <plan> <task>
```

Output is a chronological list with timestamps, event type, and the
variant/session that recorded each event.

## Plan inspection

### List plans

```sh
radioactive_ralph plan ls
```

Lists every plan in the durable store across all statuses (draft,
active, paused, done, failed-partial, archived, abandoned). Pass
`--all` to include archived + abandoned plans, or `--all-repos` to
widen the view to every repo in the operator state dir.

### Show one plan

```sh
radioactive_ralph plan show <id-or-slug>
```

Renders plan metadata (slug, title, primary variant, status, task
counts by status) and the task list. Accepts a full UUID or the plan
slug.

### List tasks in a plan

```sh
radioactive_ralph plan tasks <id-or-slug>
radioactive_ralph plan tasks <id-or-slug> --status running
radioactive_ralph plan tasks <id-or-slug> --status blocked --status failed
```

Lists tasks in status-priority order. `--status` is repeatable; pass
it once per status you want to include (`pending`, `ready`,
`ready_pending_approval`, `blocked`, `running`, `done`, `failed`,
`skipped`, `decomposed`).

### Show the next ready task

```sh
radioactive_ralph plan next <id-or-slug>
```

Prints the next task the scheduler would claim for the given plan —
the head of the ready set after dependency resolution. Useful for
dry-running "what would the service pick up next?" without starting a
worker.

## Force-complete a task from the operator surface

When a task is stuck in a non-running state (blocked, failed,
`ready_pending_approval`, or pending) and the operator wants to mark
it done without going through a worker session:

```sh
radioactive_ralph plan mark-done <id-or-slug> <task-id>
radioactive_ralph plan mark-done <id-or-slug> <task-id> --evidence "manual verification passed"
```

This is the operator analogue of the worker-side `MarkDone` and emits
a `completed_operator` event in `task_events`. For a task already in
`running` state under a live session, the worker-side path applies
and `mark-done` delegates to it. Downstream tasks become claimable
once the force-completed task is `done`.
