# Phase 1: Code Quality & Architecture Review

Full raw findings: `raw-1a-code-quality.md` (Step 1A) and `raw-1b-architecture.md` (Step 1B).

Combined totals — Code Quality: **2 Critical, 5 High, 6 Medium, 4 Low**. Architecture: **2 Critical, 4 High, 5 Medium, 3 Low**. Several findings overlap (marked below).

## Code Quality Findings

### Critical
- **Q-C1 — Swallowed errors on the durable task-state write path.** `internal/runtime/service.go:453-566`: every terminal task-state transition (`MarkDone`, `RequeueTaskWithPayload`, `MarkFailedWithPayload`) discards its error. A failed write (DB locked, disk full) orphans the task as `in_progress` forever with no log line. Fix: log via `s.logEvent` (as the `worker.error` branch already does) and leave the task reclaimable.
- **Q-C2 — Two parallel task systems; the legacy one is half-dead.** `internal/db/db.go:218-363` vs `internal/plandag/task.go`. `db.ClaimTask`/`FinishTask` are called nowhere outside their own tests. Fix: make plandag the single source of truth; reduce `internal/db` to the event log. *(Same root as Arch H1/H2.)*

### High
- **Q-H1 — `tui.go` is a 1,405-line god file** that also reaches past IPC straight into the plandag DB (`loadQueueSnapshot`, `store.TaskDeps`). Split and route reads through IPC.
- **Q-H2 — Store-open/resolve boilerplate duplicated 13×** across `cmd/radioactive_ralph/plan.go` `Run` methods. Extract a `withResolvedPlan` helper.
- **Q-H3 — `context.Background()` at 11 sites in `service.go`** (shutdown/heartbeat/dispatch) discards cancelation; a hung SQLite write blocks graceful termination indefinitely.
- **Q-H4 — `executeWorker` mixes six responsibilities in 86 lines** (`service.go:495-580`). Extract `buildRequest` and `applyOutcome`.
- **Q-H5 — `db.EnqueueTask` swallows a real FTS error** (`db.go:253-257`) with a bare `_ = err`; dedup silently degrades. *(Moot if Q-C2 deletes the path.)*

### Medium
- **Q-M1 —** `fixit.Score` is a 139-line signal cascade with inline magic weights (`internal/fixit/scorer.go:17-155`); convert to a declarative rule table.
- **Q-M2 —** Six plandag operator methods copy-paste the tx-update-audit skeleton (`task.go:574-777`); extract `operatorTransition`.
- **Q-M3 —** `RowsAffected` errors discarded across the DAG store (`task.go:598, 635, 673, 711`); driver errors masquerade as "task not requeueable."
- **Q-M4 —** `fixit/pipeline.go:144-148` drops the refinement-history write error despite a comment claiming it's logged.
- **Q-M5 —** `HandleAttach` polls the event DB every 500ms (`runtime/handler.go:54-80`); switch to write-notification with ticker backstop.
- **Q-M6 —** `service.go` at 902 lines owns too many subsystems. *(Same as Arch H4.)*

### Low
- **Q-L1 —** `_ = c.Yes` / `_ = rc` noise in `cmd/radioactive_ralph/init.go:37-38`.
- **Q-L2 —** `//nolint:nilerr` best-effort walks hide real IO errors (`internal/fixit/explore.go:127, 307`).
- **Q-L3 —** `MustRegister` panic in `internal/variant/registry.go:37` — verify registration is package-load only.
- **Q-L4 —** `payload, _ := json.Marshal(parsed)` at `service.go:539` feeds the durable state writes silently.

## Architecture Findings

### Critical
- **A-C1 — Durable `service start` path enforces none of the safety gates the attached `run` path enforces.** `run.go:60-102` checks `HasGate`/`gateConfirmed`/`RequireSpendCap`; `ServiceStartCmd.Run` + `internal/runtime` contain zero gate checks. The durable service can spawn destructive gated variants (savage, old-man, world-breaker) with no confirmation and no cap. Fix: one admission-control gate in `internal/runtime` at the `startWorker`/`chooseProfile` boundary.
- **A-C2 — Spend cap is declared but never enforced.** No code accumulates spend; the `db.spend` table has zero readers/writers; `provider.Result` can't even carry token usage. A `$5` cap does nothing. Fix: extend `Result` with `Usage`, accumulate per session, hard-stop at cap — or remove the theater.

### High
- **A-H1 — Two overlapping SQLite data models** (`internal/db` tasks/sessions/spend vs plandag). Verified near-zero live consumers of the db-side API. *(Same as Q-C2.)*
- **A-H2 — IPC `enqueue` writes to the orphaned queue**; the scheduler never reads it — enqueued work is silently dropped. Remove `CmdEnqueue` or re-point at plandag.
- **A-H3 — IPC protocol has no version field** despite long-lived service installs guaranteeing client/server version skew. Mirror plandag's schema-version discipline.
- **A-H4 — `runtime/service.go` god object** (902 lines, 32 methods). Decompose into scheduler + worker/executor. *(Same as Q-M6/Q-H4.)*

### Medium
- **A-M1 —** `provider.Result` lacks a `Usage` field — the structural blocker for A-C2.
- **A-M2 —** Advertised `mcp__radioactive-ralph__*` tool surface does not exist in the Go code (no MCP server, no jsonrpc dep); phantom API in the described contract.
- **A-M3 —** Test harness code (`fakeclaude`, `cassette/`) lives in the production `provider/claudesession` tree.
- **A-M4 —** `plandag/task.go` ~925-line single file; split along type/claim/transition/scan seams.
- **A-M5 —** DAG acyclicity enforced only in Go (`wouldCreateCycle`); schema has just a self-edge CHECK. `plan import` should re-validate.

### Low
- **A-L1 —** Provider defaulting duplicated between `ResolveBinding` and `builtInProvider` (`provider.go:43-100`).
- **A-L2 —** Heavy build-tag surface in Windows/Unix service-host split (4 `service_windows*` variants).
- **A-L3 —** `db.tasks` timestamp parse errors swallowed (`db.go:355-356`); moot if the table is deleted.

## What is healthy (verified by both reviewers)

- Clean, acyclic internal dependency graph; correct dependency direction throughout.
- Right-sized provider abstraction faithfully implementing "providers are bindings."
- Strong data-driven variant profile system with fail-fast `Validate()` (registration-time only — see A-C1).
- Correct plandag migration story (embedded transactional migrations, `user_version`, newer-DB refusal).
- Well-modeled composite-key task/dep schema with audit/heartbeat separation.
- Consistent `%w` error wrapping and transactional operator state transitions.

## Critical Issues for Phase 2 Context

1. **A-C1 (security-relevant):** The durable service path bypasses all confirmation gates and write-isolation declarations for destructive variants — the security review should treat `internal/runtime` dispatch as an unguarded privilege-escalation surface and examine what a hostile or malformed plan file can cause the service to execute.
2. **A-C2 (security/cost-relevant):** Spend caps are a no-op — resource-exhaustion / runaway-cost scenarios are unmitigated.
3. **Q-C1 (reliability):** Durable-state writes fail silently — relevant to performance review of the SQLite write path (busy timeouts, contention) since a locked DB currently means silent task orphaning.
4. **A-H2:** IPC `enqueue` is a dead path — security review should check whether the socket surface exposes other unvalidated or unused commands.
5. **A-H3:** No IPC protocol versioning — malformed/mismatched frames deserve fuzz-style scrutiny in the security pass.
6. **Q-M5:** 500ms polling in `HandleAttach` — performance review should quantify event-streaming latency/scan cost as event volume grows.
