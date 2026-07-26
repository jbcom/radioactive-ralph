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

## Definitions

A durable task is not a worker. Import may create any number of task rows. A
task consumes simultaneous capacity only while it has an active admission
reservation and a corresponding running owner.

The SQLite connection-pool size is unrelated to worker concurrency.
`RALPH_MAX_PARALLEL` remains a final process-local emergency ceiling around
provider goroutines. It is applied after durable policy admission and never
substitutes for measured dimensions.

“Global” in a project policy means project-global. A provider or local
inference domain shared by several projects also needs a host-scoped guard;
otherwise individually safe project policies can overload the shared host.

## Work classes

Every new v2 task requires a stable `workClass`. It is an authored
classification of workload and resource behavior, not a provider, team,
persona, or inferred task-name category. No class carries a default count.

Stored experimental v2 tasks migrate to `legacy-v2`; legacy plans keep their
existing behavior. Import validation and stored-plan compatibility validation
are separate so adding the required field does not make historical plan source
undispatchable.

## Versioned policy

Each project has an immutable sequence of policy documents and one current
head. A document has schema version 1 and independent positive limits for:

- project-global in-flight work;
- a `workClass`;
- a calibrated provider alias;
- a calibrated `independence_domain`.

The project-global key is canonical `*`. A missing row means unbounded at that
dimension. Zero never means unbounded.

Whole-document replacement uses optimistic concurrency against the expected
policy version. The store validates and canonically hashes the complete
document, inserts the new immutable version, and updates the head in one
transaction. Tightening a policy does not kill admitted work; existing
reservations keep counting and new claims wait.

## Durable reservations and throttle state

An admitted task owns one reservation containing:

- project, plan, task, session, and worker IDs;
- pinned policy version;
- `workClass`;
- calibrated alias and independence domain;
- creation time.

Reservations survive supervisor restart. They are not cascade-deleted by
worker/session cleanup; every lifecycle path releases them explicitly in the
same owner-guarded task transaction.

Throttle observations are durable but do not change task status. A ready task
may carry several ordered reasons:

```text
work_class:visual-critique 2/2
independence_domain:ollama-macmini 1/1
```

Throttle is not `blocked_capability`. Events are emitted only when the reason
set materially changes, avoiding a one-second dispatch-loop flood.

## Atomic admission

Before claim, the orchestrator resolves immutable prospective provenance:
alias, provider, model, effort, calibration ID, and calibrated independence
domain. A provisional provider type is not empirical independence provenance.

One immediate SQLite transaction:

1. verifies project, plan, exact task, readiness, dependencies, and owner;
2. verifies `workClass` matches durable metadata;
3. validates file/artifact reservation conflicts;
4. reads the current policy head and host-domain guard;
5. counts all active reservations across policy versions;
6. evaluates every applicable project and host dimension;
7. either records throttle reasons without changing task status, or:
8. marks the task running, assigns session/worker, inserts the reservation,
   records complete provider provenance, assigns the worker task, clears
   throttles, and emits the claim event;
9. commits.

Admission succeeds only when every applicable dimension has capacity.
Policy-replace and claim races serialize through the same immediate-writer
discipline.

## Release and recovery

The same owner-guarded transaction releases the reservation for:

- done;
- retryable and terminal failure;
- system claim release;
- capability/input blocking;
- panic cleanup;
- operator kill/reclaim;
- stale-worker recovery.

A stale session cannot delete a newer owner's reservation. Reopen continues to
count live reservations; orphan recovery requeues the exact task and releases
its reservation atomically.

Real process PID/start-time provenance is required for strong abrupt-crash
proof. Placeholder worker PIDs are not sufficient evidence that an orphan
process is gone.

## IPC and clients

Authenticated local IPC exposes:

- `admission-policy-get`;
- `admission-policy-replace`.

Status reports durable task count, active worker count, active reservation
count, policy version, dimension/key limits and occupancy, distinct queued
tasks, each task's `workClass`, and exact throttle reasons. Telemetry failure
does not become a fabricated zero.

The TUI remains read-only. The GUI may replace a policy. Both consume the same
typed status mapping so occupancy and reasons cannot drift between clients.

## Empirical calibration

Provider capability calibration and admission calibration are distinct. An
admission calibration run is keyed by:

- work class;
- exact provider alias and immutable provider calibration ID;
- independence domain;
- fixture digest;
- host fingerprint and pressure envelope;
- calibration algorithm version.

Each sample persists throughput, latency distribution, provider
throttling/retries/timeouts, verification and watchdog failures, dispatch/DB
delay, spend, and host CPU/memory/swap/load observations.

The search procedure is:

1. start at the minimum valid probe;
2. grow geometrically only while safety predicates pass and throughput
   improvement exceeds measured noise;
3. stop at the first unsafe or flat observation, or an operator search
   ceiling;
4. bracket the last safe and first unsafe point;
5. refine with integer midpoints and repeated steady-state samples;
6. select the highest proven-safe point below the empirical knee with
   persisted headroom.

Overlapping confidence intervals require more repetitions. Provider
throttling, latency inflation, throughput regression, verification errors,
watchdog failures, critical host pressure, dispatch lag, or spend ceilings are
unsafe evidence. If the search reaches its ceiling while still improving, the
result is **censored**, not optimal.

## Host pressure and shared domains

The supervisor needs a platform pressure sampler and fresh durable snapshot.
An adaptive policy that requires pressure evidence throttles new admission
when the snapshot is stale, the host fingerprint changes, or the observation
leaves the calibrated envelope.

Project policies intersect with host-scoped provider/domain guards. This is
required for local Ollama and any cloud quota shared across projects. Each
reservation pins both effective policy references.

## Implementation DAG

1. **Grammar and compatibility** — `workClass`, strict import, stored-plan
   compatibility, migration/backfill.
2. **Policy store** — immutable documents, CAS replacement, unbounded baseline.
3. **Atomic claim and lifecycle** — reservations, throttles, provenance, every
   release/recovery path.
4. **Orchestrator reorder** — exact provider/domain resolution before claim.
5. **IPC and clients** — typed policy/status, common TUI/GUI view.
6. **Pressure and knee calibration** — samples, safety classification,
   confidence/headroom.
7. **Cross-project host guards** — intersect project and shared-domain policy.
8. **Full proof and documentation**.

## Required proof

- Import rejects missing/invalid `workClass` with zero rows while stored v2 and
  legacy plans remain usable.
- Migration seeds an unbounded baseline and backfills live reservations without
  losing owners.
- Policy replacement is immutable, validated, hash-stable, and CAS-guarded.
- Different classes receive independently measured limits without a baked-in
  worker constant.
- Aliases sharing one independence domain contend; independent domains do not.
- Shared readers remain concurrent until policy, not file reservation, limits
  them.
- High-contention claims never exceed any single or combined dimension.
- Every running v2 task has exactly one owner-matching reservation and no
  reservation exists without its running owner.
- Claim versus policy replacement pins either the old or new complete version.
- Done, retry, failure, release, block, panic, kill, stale reclaim, orphan
  recovery, and reopen leak no reservations.
- A stale former owner cannot release current capacity.
- Status, IPC, TUI, and GUI show identical ordered throttle reasons.
- Synthetic curves cover a clear knee, plateau, noisy overlap, provider error,
  pressure failure, and censored search.
- No production code or test treats a particular task count, worker count,
  emergency ceiling, or search ceiling as optimal.
