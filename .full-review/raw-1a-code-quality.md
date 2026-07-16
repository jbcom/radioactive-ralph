# Step 1A Raw Findings: Code Quality Review

**Summary: 2 Critical, 5 High, 6 Medium, 4 Low**

The Go codebase is generally well-structured with idiomatic error wrapping (`%w`), transactional DB writes, and good comment discipline. The most serious issues are (1) silently swallowed errors on the durable-state write path in the runtime worker, and (2) two parallel, partially-dead task-tracking systems.

## Critical

### C1. Swallowed errors on the durable task-state write path
**File:** `internal/runtime/service.go:453-566` (also 488, 491, 469)
**Category:** Error handling

Every terminal task-state transition in the worker lifecycle discards its error return. `executeWorker` is where a task's outcome ("done", "handoff", "blocked", "failed") is persisted to the durable plan DAG — the entire point of the system:

```go
case "done":
    _, _ = s.planStore.MarkDone(ctx, plan.ID, task.ID, s.sessionID, string(payload))
case "handoff":
    _ = s.planStore.RequeueTaskWithPayload(ctx, plan.ID, task.ID, s.sessionID, taskPayload, parsed.HandoffTo, parsed.ApprovalRequired)
...
default:
    _, _ = s.planStore.MarkFailedWithPayload(ctx, plan.ID, task.ID, s.sessionID, taskPayload, maxRetries)
```

If any of these fail (DB locked, disk full, tx conflict), the task silently remains `in_progress` forever. The worker goroutine exits, `finishWorker` clears the session-variant link, and the task is orphaned with no log line and no retry. `startWorker:456` and `finishWorker:488,491` have the same pattern for `MarkFailed` and `ReleaseWorktree`.

**Fix:** At minimum, log every failed state write via the existing `s.logEvent`; ideally leave the task claimed so a heartbeat sweep can reclaim it. Example:

```go
if _, err := s.planStore.MarkDone(ctx, plan.ID, task.ID, s.sessionID, string(payload)); err != nil {
    _ = s.logEvent(ctx, "worker.state_write_failed", map[string]any{
        "plan_id": plan.ID, "task_id": task.ID, "outcome": "done", "error": err.Error(),
    })
}
```
The `worker.error` branch at `service.go:522` already does this correctly — apply the same treatment to all switch arms.

### C2. Two parallel task systems; the legacy one is half-dead
**Files:** `internal/db/db.go:218-363` vs `internal/plandag/task.go` (whole file)
**Category:** Technical debt / duplication

There are two independent task models with their own SQLite tables and lifecycle verbs:

- `internal/db` — `tasks` table with `EnqueueTask`/`ClaimTask`/`FinishTask`/`ListTasks` (queued→running→done/failed).
- `internal/plandag` — the durable DAG with `CreateTask`/`ClaimNextReady`/`MarkDone`/`MarkFailed`/`OperatorRequeueTask`/... (pending→ready→in_progress→done/blocked/...).

The runtime uses `plandag` for the real work; `db.EnqueueTask` is still wired to `HandleEnqueue` (`handler.go:33`), but `db.ClaimTask` and `db.FinishTask` are **called nowhere outside their own tests** (verified: only `internal/db/db_test.go` references them). The result is two conflicting mental models of "a task," a dedup/FTS path (`db.go:241-257`) that no live claim path consumes, and a real risk that a future contributor extends the wrong one.

**Fix:** Decide the single source of truth (plandag is clearly it). Either delete the dead `db.Task` lifecycle (`ClaimTask`, `FinishTask`, `ListTasks`, the `Task` struct, the dedup query) and reduce `internal/db` to the event log + spend + session tables it still owns, or route `HandleEnqueue` into plandag and remove the `tasks` table entirely. Do not leave both.

## High

### H1. `tui.go` is a 1,405-line god file
**File:** `cmd/radioactive_ralph/tui.go`
**Category:** Maintainability / cohesion

This single file owns: the Bubble Tea model, 10+ message types, key-handling state machine (`updateNormal`, `View`, `updateInput`), socket lifecycle (`ensureAlive`, service spawn at 1173-1219 with raw `os.Remove`/`logFile.Close` juggling), **and** direct plandag store access (`loadQueueSnapshot` at 1028, 107 lines). A reader cannot hold it in their head, and it reaches past the IPC layer straight into the DB (`store.TaskDeps` at 1076), duplicating query logic that belongs in plandag or the service.

**Fix:** Split into `tui_model.go` (model + update + view), `tui_process.go` (service spawn/socket management), and move `loadQueueSnapshot`/`TaskDeps` reads behind an IPC status call so the TUI stops opening its own store handle.

### H2. Store-open/resolve boilerplate duplicated 13× across plan commands
**File:** `cmd/radioactive_ralph/plan.go` (13 `Run` methods; 40 matches of the triad)
**Category:** Duplication / DRY

Nearly every `PlanXxxCmd.Run` opens with the identical 10-line preamble:

```go
store, err := openPlanStore(rc.ctx)
if err != nil { return err }
defer func() { _ = store.Close() }()
repo, err := resolveRepoRoot("")
if err != nil { return err }
plan, err := resolvePlan(rc.ctx, store, c.PlanIDOrSlug, repo)
if err != nil { return err }
```

**Fix:** Introduce a `withResolvedPlan(rc, ref, fn)` helper that hands the closure a ready store + resolved plan. Each command body collapses to a single `store.OperatorXxx(...)` + print.

### H3. `context.Background()` used where a cancelable context is in scope
**File:** `internal/runtime/service.go:360, 365, 398-399, 696, 704` (11 sites)
**Category:** Error handling / resilience

Shutdown, heartbeat, and dispatch paths call `s.planStore.CloseSession(context.Background(), ...)`, `HeartbeatSession(context.Background(), ...)`, `ListPlans(context.Background(), ...)`, etc., discarding the service's cancelation signal. On shutdown these DB calls cannot be interrupted, so a hung SQLite write blocks graceful termination indefinitely.

**Fix:** Thread the service's `workerCtx` (created at line 156) or a short `context.WithTimeout` derived from it into these calls. Reserve `context.Background()` only for genuinely detached cleanup that must outlive cancelation (and comment why).

### H4. `executeWorker` mixes six responsibilities in one 86-line method
**File:** `internal/runtime/service.go:495-580`
**Category:** Complexity / SRP

`executeWorker` resolves the provider binding, builds the runner, constructs both prompts + schema + model + effort, runs the subprocess, records provider metadata, parses the result, and fans out a 6-arm outcome switch that writes durable state.

**Fix:** Extract `buildRequest(profile, plan, task) provider.Request` and `applyOutcome(ctx, plan, task, binding, parsed) error`. The latter isolates the switch so its error handling can be tested and centralized.

### H5. `db.EnqueueTask` deliberately swallows a real FTS error
**File:** `internal/db/db.go:253-257`
**Category:** Error handling

```go
if err != nil && !errors.Is(err, sql.ErrNoRows) {
    // FTS query errors shouldn't block inserts.
    _ = err
}
```

A malformed-FTS or corrupted-index error is silently dropped. The dedup guarantee silently degrades to "sometimes duplicates," with zero operator visibility.

**Fix:** Keep the fall-through (dedup is best-effort) but surface the signal via a logger or debug event. (If C2 is resolved by deleting this path, this finding disappears with it.)

## Medium

### M1. `fixit.Score` is a 139-line linear signal cascade
**File:** `internal/fixit/scorer.go:17-155`

Flat sequence of `if signal { scores["x"].Score += N }` blocks with hard-coded variant names and magic weights inline.

**Fix:** Move base scores and per-signal deltas into a declarative table (`[]scoringRule{Signal, Variant, Delta, Reason}`) and iterate.

### M2. `plandag` operator methods share a copy-pasted tx-update-audit skeleton
**File:** `internal/plandag/task.go:574-777` (`OperatorRequeueTask`, `OperatorRetryTask`, `OperatorFailTask`, `OperatorSkipTask`, `OperatorDecomposeTask`, `OperatorMarkDone`)

Six operator methods repeat: `BeginTx` → `defer Rollback` → `UPDATE tasks SET status=...` → check `RowsAffected==0` → `INSERT task_events` → `Commit`.

**Fix:** Extract `operatorTransition(ctx, planID, taskID, newStatus, allowedFrom, eventType, payload)` — ~120 lines collapse to ~30.

### M3. `RowsAffected` errors discarded across the DAG store
**File:** `internal/plandag/task.go:598, 635, 673, 711` (and peers)

`n, _ := res.RowsAffected()` drops the error, then branches on `n == 0` — a driver error masquerades as "task not requeueable."

**Fix:** Capture and wrap the error. `db.go:282` already does this — make the DAG store consistent.

### M4. `pipeline.go` bare `_ = err` discards refinement-history write failure
**File:** `internal/fixit/pipeline.go:144-148`

Comment says "log via the returned EmittedPlan," but nothing is logged — the error vanishes.

**Fix:** Attach the error to `EmittedPlan` (a `HistoryWriteErr` field) as the comment intends, or emit a warning through the pipeline's writer.

### M5. Polling ticker for event streaming
**File:** `internal/runtime/handler.go:54-80`

`HandleAttach` polls the event DB every 500ms, adding up to 500ms latency to every streamed event and doing wasted `Replay` scans while idle.

**Fix:** Publish new-event notifications through a channel (fan-out on `Append`) so attach wakes on write; keep the ticker only as a liveness backstop.

### M6. `service.go` at 902 lines owns too many subsystems
**File:** `internal/runtime/service.go`

Owns lifecycle, config reload, worker dispatch/execution, workspace acquisition, heartbeating, status assembly, and event logging.

**Fix:** Split worker dispatch/execution into `worker.go` and status assembly into `status.go`.

## Low

### L1. `init.go` uses `_ =` to suppress unused params
**File:** `cmd/radioactive_ralph/init.go:37-38` — `_ = c.Yes` / `_ = rc`. Remove or wire the unused fields.

### L2. `//nolint:nilerr` best-effort walks hide real IO errors
**File:** `internal/fixit/explore.go:127` (and 307) — permission-denied on repo root yields silently empty results. Count skipped errors and surface the count.

### L3. `panic` in variant registration
**File:** `internal/variant/registry.go:37` — `MustRegister` panics on duplicate/invalid variant. Legitimate init-time invariant; confirm all registration happens at package load, never on a request path.

### L4. `parsed`/`payload` marshal error dropped
**File:** `internal/runtime/service.go:539` — `payload, _ := json.Marshal(parsed)`. A future non-marshalable field would write an empty payload to durable state silently; feeds the C1 state writes.

## Notes on what's healthy
- Error wrapping with `%w` is consistent in the DB and store layers, giving good error chains.
- Operator state transitions are correctly transactional (update + audit-insert in one tx).
- `defer func() { _ = x.Close() }()` usage is idiomatic, not a defect.
- Comment discipline is strong; most non-obvious decisions are explained inline.

Priority files: `internal/runtime/service.go` (C1, H3, H4, M6), `internal/db/db.go` + `internal/plandag/task.go` (C2, M2, M3), `cmd/radioactive_ralph/tui.go` / `plan.go` (H1, H2).
