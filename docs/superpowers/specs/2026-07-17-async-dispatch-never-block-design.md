# Async dispatch — restoring the never-block invariant

## Problem

The supervisor's central invariant is "the system can never block/wedge." A
comprehensive-review pass found it is currently violated on the hottest path.

`Supervisor.dispatchActivePlans` (internal/supervisor/supervisor.go) takes
`dispatchMu` and holds it across a loop calling `Orchestrator.DispatchNext` for
every active plan. `DispatchNext`'s own loop calls `dispatchWorker` **inline**
(internal/orch/orchestrator.go), and `dispatchWorker` runs `runner.Run` — the
actual provider agent turn — **synchronously and blocking**, bounded only by
`watchdogConfig.StallTimeout` (default **5 minutes**, never overridden).

So a single slow provider turn holds `dispatchMu` for up to `StallTimeout` per
dispatched step. During that window:

- `HandleEnqueue` (an interactive IPC call) also takes `dispatchMu`, so any
  client "wake up" / enqueue hangs for minutes.
- The periodic tick goroutine that runs `dispatchActivePlans` is the **same**
  goroutine that runs `ReclaimStale` + `HeartbeatSession`, so the reaper cannot
  reclaim crashed workers on *other* plans while one plan dispatches slowly.

The doc comment on `DispatchNext` already asserts the intended design — "each
dispatch runs its provider turn in its own goroutine ... never wait" — but the
code contradicts its own contract. This is the fix that makes the code match the
documented, correct behavior.

## Design

Make `dispatchWorker` run asynchronously, matching the doc contract, while
preserving the three properties that the current synchronous form provides for
free:

1. **`maxParallel` still bounds concurrency.** Synchronous dispatch bounds
   concurrent provider turns by simply blocking; async dispatch must bound them
   with a **weighted semaphore** (buffered channel of size `maxParallel`)
   acquired before launching each `dispatchWorker` goroutine and released when it
   returns. `DispatchNext` acquires non-blockingly (try-acquire): if the semaphore
   is full, there is no free capacity this pass, so it stops dispatching (the next
   tick / enqueue picks up where it left off) rather than blocking the lock.

2. **`dispatchMu` guards only the fast claim phase.** The claim work
   (`spawnWorkerRows`, `claimStepTask`, `SetWorkerTask`) is quick store I/O and
   stays synchronous inside the loop, under the lock. Only the slow tail —
   `dispatchWorker`'s `runner.Run` + `recordUsage` + evidence append +
   `VerifyAndComplete` — moves into the goroutine, off the lock.

3. **Shutdown drains in-flight work.** Add a `sync.WaitGroup` to the
   orchestrator; every launched `dispatchWorker` goroutine is tracked. A new
   `Orchestrator.Wait()` (or `Drain(ctx)`) blocks until in-flight goroutines
   finish; the supervisor's `shutdown` calls it after the run loop breaks and
   after cancelling `runCtx` (which ctx-cancels the in-flight `runner.Run`s via
   the existing `runningWorkers` registry, so drain is bounded, not a hang).

### Error handling

`dispatchWorker` currently returns an `error` consumed by `DispatchNext`. Async,
it cannot return to the caller — so its errors are **logged + emitted** as a
store event (`worker.dispatch_error`, stream `service`), the same way spend-cap
refusals are already surfaced. A dispatch error for one worker must never abort
the pass or crash the supervisor; the never-block invariant means best-effort.

### Concurrency notes

- The semaphore is an orchestrator field, sized from `maxParallel` at
  construction. `maxParallel` is a store-wide cap on concurrent provider turns
  (not per-plan) — which matches the current behavior where the shared
  orchestrator's synchronous dispatch serialized all plans anyway.
- `runningWorkers` (cancel registry) and the new `WaitGroup` are distinct: the
  registry lets `KillWorker` cancel one turn; the WaitGroup lets shutdown wait for
  all turns. Both are needed.
- The `dispatched` count returned by `DispatchNext` now means "steps *launched*
  this pass," not "steps completed" — which is already what the count is used for
  (logging + the enqueue reply's `Inserted` heuristic), so no caller breaks.

## Tests

1. **Tick does not block on a slow provider.** A fake runner that blocks on a
   channel; assert `dispatchActivePlans` returns promptly (the goroutine is still
   running) and a second call / `HandleEnqueue` does not block.
2. **`maxParallel` bounds in-flight turns.** With `maxParallel=2` and 5 ready
   steps + a blocking runner, exactly 2 turns start; releasing them lets the rest
   proceed on subsequent passes.
3. **Shutdown drains.** Launch a worker on a runner that completes after a short
   delay; `Wait()` returns only after the verify write lands.
4. **Dispatch error is emitted, not fatal.** A runner/verify that errors emits a
   `worker.dispatch_error` event and the pass still returns without error.

Existing E2E and orchestrator tests must stay green (many rely on synchronous
completion — those will need a `Wait()` call after dispatch to observe results
deterministically, which is the correct new contract).
