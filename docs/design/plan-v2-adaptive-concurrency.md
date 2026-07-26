---
title: Plan v2 adaptive concurrency follow-on
description: Blocking design seam for measured per-work-class and independence-domain admission.
---

# Plan v2 adaptive concurrency follow-on

`RALPH_MAX_PARALLEL` is a supervisor-wide safety ceiling. It is not an
optimizer and cannot represent a measured policy such as “run more schema
checks than visual critiques, but never run two tasks against the same local
inference domain.” A plan with heterogeneous work must not collapse those
measurements into one conservative global number, and Ralph must not hard-code
a worker target such as 16.

Adaptive policy is therefore a blocking follow-on before a heterogeneous v2
plan can claim calibrated optimal scheduling. The v2 DAG, task store, and
calibrated provider work in this release remain task-count unbounded; the
follow-on changes admission, not graph capacity.

## Required contract

1. Add a stable `workClass` to every v2 task. It is an authored scheduling
   category such as `schema-validation`, `systems-design`, or
   `visual-critique`, not a provider name or persona.
2. Store a versioned policy per project with independent limits for:

   - all running tasks in the project;
   - each `workClass`;
   - each calibrated provider alias when needed;
   - each calibrated `independence_domain`.

   Missing limits mean unbounded at that dimension. A stored policy never
   invents a default worker count.
3. Move policy admission into the same SQLite transaction that claims the
   exact task and reserves its inputs/outputs. The prospective calibrated
   independence domain must be known before this transaction. A process-local
   semaphore is insufficient because it cannot prove a 200-way claim race or
   survive supervisor restart.
4. Persist an admission reservation keyed by plan/task/claiming session with
   the policy version, work class, alias, and independence domain. Completion,
   failure, reclaim, and operator kill release it through the existing
   owner-guarded task transition.
5. Add authenticated IPC operations to get and replace a project policy.
   Replacement is dynamic and durable. Tightening a policy does not kill
   already-admitted work; it prevents new claims until observed occupancy is
   below the new limit.
6. Extend status with limit/occupancy/queued counters for global, class, alias,
   and independence-domain dimensions. The TUI and GUI team drilldowns show
   which dimension throttled a ready task without misclassifying it as a
   capability failure.

`maxParallel` remains a final emergency ceiling around provider goroutines. It
must be applied after durable policy admission and must not substitute for any
of the dimensions above.

## Implementation seams

| Concern | Seam |
| --- | --- |
| Plan grammar | `internal/plan.TaskMetadata`, strict required-field parsing, v2 validation, plan-format docs |
| Durable policy and reservations | a new migration plus `internal/store` policy/claim APIs |
| Atomic admission | `internal/store.ClaimReadyTask`; combine dependency, file reservation, and policy predicates in one transaction |
| Prospective provenance | `internal/orch.resolveV2DispatchBinding`; resolve calibrated independence before claim |
| Lifecycle release | existing owner-guarded done/fail/reclaim/kill transitions in `internal/store` |
| Runtime configuration | versioned `internal/ipc` get/replace commands implemented by `internal/supervisor` |
| Observability | status payload, store rollups, TUI team view, GUI team view |

## Required proof

- Import rejects missing/invalid `workClass` with zero rows and preserves
  legacy plans unchanged.
- Two different work classes can consume different limits concurrently.
- Two aliases sharing one independence domain consume the same domain budget;
  aliases in different domains do not.
- Shared readers remain concurrent until a policy dimension, not a file
  reservation, is exhausted.
- A 200-claimer race never exceeds global, class, alias, or domain limits.
- Policy replacement during active work is race-clean and becomes effective
  on the next claim.
- Reclaim, retry, failure, completion, and supervisor restart do not leak
  reservations.
- Status, IPC, TUI, and GUI report identical occupancy and throttle reasons.
- No test or production code assumes a fixed task count or worker target.
