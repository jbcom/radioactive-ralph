# Step 2B Raw Findings: Performance & Scalability Analysis

**Summary: 1 Critical, 6 High, 7 Medium, 5 Low.** Verified runtime shape: one global `plans.db` at XDG `StateRoot()/plans.db` shared by every repo service, the TUI, and the CLI (separate `sql.DB` handles across processes); per-repo event log `state.db`; scheduler tick default 1s.

## CRITICAL

### P-C1. Durable-state writes silently discarded under contention — task outcomes can be lost
**`internal/runtime/service.go:398-399, 415, 469, 488, 553-566`** (`_ =` / `_, _ =` on `HeartbeatSession`, `AttachPlan`, `SetSessionVariantTask`, `ClearSessionVariantTask`, `MarkDone`, `MarkFailed*`, `RequeueTaskWithPayload`, `MarkBlocked`).

`plans.db` is written cross-process. All plandag transactions are **deferred** (`BeginTx(ctx, nil)`), so a SELECT-then-UPDATE tx racing another process's commit gets `SQLITE_BUSY_SNAPSHOT`, which `busy_timeout=5000` does **not** retry — it fails immediately, and the error is discarded. A worker finishes an expensive run, `MarkDone` fails, the task is stuck `running` forever (no reaper — see P-H3). Grows with concurrent writers × per-second write frequency. *(Same defect as Phase-1 Q-C1 and security S-H4, root-caused to SQLite locking here.)*

**Fix:** (a) immediate transactions for read-modify-write paths — modernc supports `_txlock=immediate` in the DSN so `busy_timeout` actually serializes writers; (b) never discard these errors — retry with backoff and log `worker.persist_failed`.

## HIGH

### P-H1. Event log grows unbounded; every attach client replays the entire history
`internal/runtime/handler.go:53-61` (`lastID` starts at 0 → first tick replays all events); no `DELETE FROM events` anywhere; schema stores both `payload_parsed` and `payload_raw` per event. Startup cost is C clients × E events full-row scans + JSON marshals; a months-old service makes attach take seconds-to-minutes. **Fix:** start `lastID` at `SELECT COALESCE(MAX(id),0)` (or a `--since` cursor); add retention (age/row-count cap); longer-term replace 500ms polling with an in-process broadcast channel fed at `Append`.

### P-H2. Git clone/fetch/worktree run synchronously on the scheduler goroutine
`internal/runtime/service.go:453-457` — `startWorker` calls `acquireWorkspace` before spawning the worker goroutine; on first use this is a `git clone --mirror` of the whole repo (`workspace.go:164-179`), plus `Reconcile` (`git worktree list`) every dispatch. A multi-GB repo blocks the scheduler for minutes: no heartbeats, no other dispatch, no idle checks. **Fix:** move workspace acquisition inside the worker goroutine; cache `Reconcile` (only needed at manager init).

### P-H3. Heartbeat/reclaim system half-built: constant write load, zero consumer, permanent task leaks
`internal/runtime/service.go:386-403` — heartbeats run on `TickInterval` (default 1s), not `HeartbeatInterval` (10s), so `HeartbeatSession` + a per-worker `HeartbeatSessionVariant` UPDATE fire every second forever. **No reaper exists** (nothing reads `last_heartbeat`; `reclaim_count` is written by nobody). On crash, sessions/session_variants rows accumulate forever and tasks claimed by the dead session stay `running` permanently. ~86k write txns/day per idle service, each an fsync. **Fix:** heartbeat on its own ticker at `HeartbeatInterval`; implement the reaper (requeue tasks whose claim heartbeat is >~3× interval stale, incrementing `reclaim_count`; delete stale sessions); batch worker heartbeats into one statement.

### P-H4. Data race on `Service.cfg`/`Service.local` between workers and config reload
`internal/runtime/service.go:496-497` — `executeWorker` (worker goroutines) reads `s.cfg`/`s.local`/`s.cfg.Variants[...]` with **no lock**; `reloadConfig` (307-314) overwrites both under `s.mu`. `config.File` holds maps, so this is a real Go data race (torn map read → crash). **Fix:** snapshot cfg/local under the mutex at worker start (or hold in an `atomic.Pointer`); run `go test -race` on a reload-during-dispatch test.

### P-H5. Fixit doc scan walks the entire repo tree unbounded (incl. node_modules, .git)
`internal/fixit/explore.go:123-148` — `scanDocs` does `filepath.WalkDir(repoRoot,...)` with **no skip list** (unlike `countLangs` at 301-324 which correctly skips `.git`/`node_modules`/`vendor`/`dist`/`build`), then walks `repoRoot/docs` a second time (double-counting docs/*.md). Every matched `.md` is fully read into memory + `os.Stat`. A JS monorepo has thousands of vendored `.md`; turns a "cheap deterministic" stage into minutes of I/O and pollutes results. **Fix:** reuse the skip set, restrict the root walk to depth 1 + full walk of `docs/` only, read only the frontmatter head (~8KB via `LimitReader`).

### P-H6. TUI reopens + re-migrates the plan store every 2s and issues N+1 queries per snapshot
`cmd/radioactive_ralph/tui.go:953-968` (2s ticker) → `loadQueueSnapshot` calls `openPlanStore` per cycle (each runs `sql.Open` + `Ping` + pragma + full `Migrate()`); inside: `TaskDeps` per task (1076), `Ready` per plan (1110); `performPlanMutation` opens a fresh store per keystroke; `ListRepoTaskSummaries` runs 2 correlated subqueries per row. ~T+P+4 queries + store open/close every 2s on the contended `plans.db`; re-runs migration checks ~43k times/day. **Fix:** open one Store at TUI start, close on quit; bulk `task_deps` query; reuse `Ready` results.

## MEDIUM

### P-M1. `ClaimNextReady` is not actually atomic across processes — task double-claim
`internal/plandag/task.go:219-287` — doc claims "BEGIN IMMEDIATE + UPDATE ... RETURNING" but uses a **deferred** tx, plain SELECT then UPDATE, and **never checks `RowsAffected`** on the claim UPDATE. If a concurrent claimer wins between SELECT and UPDATE, the second's UPDATE matches 0 rows yet still commits, logs a bogus `claimed` event, and returns the task — two workers run the same task. **Fix:** `_txlock=immediate` (per P-C1) + verify `RowsAffected()==0 → ErrNoReadyTask`. *(Correctness bug, elevate in final report.)*

### P-M2. `synchronous` pragma effectively unset — every tick/heartbeat write is a full fsync
DSNs at `internal/db/db.go:63`, `runtime/service.go:859`, `plan.go:741` set WAL + busy_timeout + foreign_keys but **not** synchronous. The `PRAGMA synchronous=NORMAL` in the schema file applies only to the migration connection (per-connection pragma; pool opens new conns freely, no `SetMaxOpenConns`). So steady-state connections run at default `FULL` — fsync ~1-2×/sec/repo forever. **Fix:** append `&_pragma=synchronous(NORMAL)` to all three DSNs; drop the misleading schema pragma lines.

### P-M3. No per-task timeout on provider subprocess runs — a hung CLI starves the variant permanently
`internal/runtime/service.go:520` — `runner.Run(ctx,...)` gets the worker context, cancelled only at shutdown. One wedged claude/codex process blocks that variant's slot (and with `AllowConcurrentVariants=false`, everything) for the service's life; heartbeats keep flowing so even a future reaper won't recover it. **Fix:** `context.WithTimeout(ctx, profile.TaskTimeout)` with a sane default; mark failed-retryable on expiry.

### P-M4. IPC accept loop exits on the first transient accept error
`internal/ipc/server.go:160-177` — comment says "log and continue" but the code logs then unconditionally `return`s. A single transient `EMFILE`/`ECONNABORTED` kills accept; status/attach/stop go dead silently (heartbeat file keeps `SocketAlive` healthy). **Fix:** `continue` after warn (small backoff), reserve `return` for `net.ErrClosed`/stop.

### P-M5. Attach streaming has no write deadline — a stalled client freezes replay mid-query
`internal/ipc/server.go:242-249` (`conn.Write`, no `SetWriteDeadline`) driven from inside `db.Replay`'s row iteration. A client that stops reading blocks `emit`, holding the events query open indefinitely — pinning the WAL from checkpointing (unbounded `-wal` growth) and leaking the goroutine. **Fix:** `SetWriteDeadline` per frame; drop on timeout; optionally buffer the batch before writing.

### P-M6. Redundant write transactions every scheduler tick
`internal/runtime/service.go:415` — `AttachPlan` runs `INSERT OR IGNORE INTO session_plans` for every active plan every 1s tick though attachment is established once. ~(1+P+W) write txns/sec/service, each contending with TUI/CLI writers (feeds P-C1) and fsyncing (P-M2). **Fix:** track attached plan IDs in a map, only `AttachPlan` unseen IDs; batch heartbeats.

### P-M7. TUI "mark done" claims an arbitrary task, not the selected one
`cmd/radioactive_ralph/tui.go:1350` — for an unclaimed task, `markDoneTaskCmd` calls `ClaimNextReady(...)` which claims whichever ready task sorts first, then `MarkDone(task.TaskID)` fails while the wrongly-claimed task is left `running` under a never-heartbeating "operator" session — a permanent leak per mistaken keypress. **Fix:** use the existing `OperatorMarkDone` for non-running tasks. *(Correctness bug.)*

## LOW

- **P-L1** — `wouldCreateCycle` (`task.go:144-183`) is a recursive N+1 query storm; import with M edges over N tasks = O(M×N) queries. Use a recursive CTE or load all edges once.
- **P-L2** — Scheduler scans all repos' plans every tick (`service.go:407-412`, `idle():696-706`); `ListPlans` has no repo filter. Add `WHERE repo_path = ?`.
- **P-L3** — Provider raw stream buffered fully in memory (`declarative.go:275-312`); long sessions → tens of MB/worker. Keep a bounded ring for error text.
- **P-L4** — Mirror never fetched after initial clone (`Manager.Fetch` has zero callers); worktrees get increasingly stale → merge-conflict churn on AI workers. Wire `Fetch` into `acquireWorkspace` (off the scheduler goroutine), rate-limited.
- **P-L5** — `reloadConfig` holds `s.mu` across `logEvent` (an insert that can stall up to busy_timeout=5s), pausing dispatch. Reorder `logEvent` after `Unlock`.

## Checked and clean
- WAL + busy_timeout + foreign_keys correctly set per-connection on all three open sites; only `synchronous` missing (P-M2).
- Incremental attach polling uses `WHERE id > ? ORDER BY id` — efficient PK range scan; steady-state poll cost negligible (only the initial full replay is the issue, P-H1).
- Index coverage matches query patterns: `tasks(plan_id,status)`, `task_deps` both directions, `task_events(plan_id,task_id,occurred_at)`, session heartbeat indexes (unused only for lack of a reaper), `plans(status)`, `plans(repo_path,slug)` unique.
- `status()` aggregates all task counts in one query — no N+1.
- FTS5 dedup uses a proper content-table with sync triggers; `ftsPhrase` sanitizes operators.
- `countLangs` correctly skips vendored dirs (contrast P-H5).
- Worktree pool bounded by `MaxParallelWorktrees`; slot reservation race-safe; stale dirs cleaned before reuse.
- Provider scanners use bounded buffers; ClaudeRunner reader goroutine terminates cleanly.
- TUI in-memory state capped (events 20, task drill-down 12).
- IPC clients short-lived one-shot; UUID v7 time-ordered keys make `ORDER BY id DESC` cheap.

**Highest-leverage sequence:** P-C1 + P-M1 + P-M2 (make the shared SQLite write path immediate-locked, checked, NORMAL-sync), then P-H3 (real heartbeat cadence + reaper), then P-H2 (unblock scheduler), then P-H1 (event retention + cursor attach).
