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
- The operator sets the state manually:
  ```sh
  radioactive_ralph plan approve-gate <plan-slug> <task-id>
  ```

### Approve a pending task

```sh
radioactive_ralph plan approvals                # list tasks awaiting approval
radioactive_ralph plan approve <plan> <task>    # release a single task
```

Approved tasks move to `ready`; the next variant to poll the DAG
claims them normally.

### Deny / requeue

If you don't want to run a pending task at all:

```sh
radioactive_ralph plan requeue <plan> <task>    # send back to pending (with retry budget reset)
radioactive_ralph plan fail <plan> <task>       # mark failed, don't retry
```

## Handoffs

### List claimed / running tasks

```sh
radioactive_ralph plan tasks <plan> --status running
```

### Hand a running task to a different variant

```sh
radioactive_ralph plan handoff <plan> <task> <new-variant>
```

What this does:

1. Kills the current variant's subprocess (gracefully — sends
   SIGTERM, waits for lifeline pipe to drain)
2. Resets the task's `assigned_variant` to the new one
3. Clears `claimed_by_session` + `claimed_by_variant_id`
4. Moves the task back to `ready`

The new variant claims it on the next poll.

### Handoff policies

Some variant profiles refuse handoffs on specific work:

- `red` (destructive) → refuses handoff TO itself unless the caller
  passes `--confirm-burn-budget`
- `world-breaker` → refuses handoff period; the task must be
  explicitly re-imported via fixit

Check the variant's `SafetyFloors` in `internal/variant/<variant>.go`
for the full rules.

## Retry budget

Every failed task keeps a retry count. The operator can manually
retry:

```sh
radioactive_ralph plan retry <plan> <task>
```

Retries bypass the variant's max-retries floor IF the operator
passes `--force`. Without `--force`, a task that has already hit its
retry ceiling fails fast instead of retrying.

## History

Every approval, handoff, retry, and failure is an event in
`task_events` (durable, per-plan). To see the history for one task:

```sh
radioactive_ralph plan history <plan> <task>
```

Output is a chronological list with timestamps, event type, and the
variant/session that recorded each event.
