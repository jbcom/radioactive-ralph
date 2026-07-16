# Step 1B Raw Findings: Architecture Review

**Summary: 2 Critical, 4 High, 5 Medium, 3 Low**

Well-structured Go codebase with a clean, acyclic internal dependency graph, a disciplined provider-binding abstraction, a data-driven variant profile system with fail-fast validation, and a properly-migrated durable plan store. Findings concentrate on: (1) a safety-enforcement gap where declared guarantees are not enforced on the durable path, and (2) an orphaned second data model (`internal/db`'s task/session/spend schema) duplicating `internal/plandag`.

## CRITICAL

### C1. Durable `service start` path enforces none of the safety gates that the attached `run` path enforces
**Files:** `cmd/radioactive_ralph/service.go` (`ServiceStartCmd.Run`), `cmd/radioactive_ralph/run.go:60-102`, `internal/runtime/service.go` (`dispatchOnce`, `startWorker`, `chooseProfile`)

Stated rule: *"Destructive variants still require explicit confirmation gates and spend-cap enforcement where declared."* This holds only on the attached CLI path (`run.go` checks `p.HasGate() && !c.gateConfirmed(p)` at lines 60-61 and `p.SafetyFloors.RequireSpendCap` at 85-101).

`ServiceStartCmd.Run` calls `NewService` with `SessionModeDurable` and **no `VariantFilter`**, and contains zero references to `HasGate`, `gateConfirmed`, `ConfirmationGate`, `RequireSpendCap`, or `WritesAllowed`. The durable scheduler (`dispatchOnce`) selects any variant a plan's task hints at via `chooseProfile`, then calls `startWorker` directly. No gate check anywhere in `internal/runtime`.

**Impact:** The durable service — the primary runtime — can spawn confirmation-gated destructive variants (savage `--confirm-burn-budget`, old-man `--confirm-no-mercy`, world-breaker `--confirm-burn-everything`) with no confirmation and no spend cap. The safety invariant lives in a UI-layer command handler and is bypassed by the intended production entry point.

**Recommendation:** Move gate/spend-cap/isolation admission control into `internal/runtime` at the `startWorker`/`chooseProfile` boundary so both attached and durable dispatch pass through one enforcement point. Pass confirmed gates/caps into `runtime.Options`; `dispatchOnce` refuses a task whose chosen profile requires an unmet gate.

### C2. `SpendCapUSD` / `RequireSpendCap` is validated-for-presence but never enforced at runtime
**Files:** `internal/config/config.go:92`, `internal/variant/fixit.go:51`, `cmd/radioactive_ralph/run.go:85-101`, `internal/db/schema/001_initial.sql:93-102` (`spend` table), `internal/runtime/service.go`

Config carries `SpendCapUSD *float64`; profiles declare `RequireSpendCap`; `run.go` errors only if the cap value is unset. But no code accumulates spend or stops a worker at the cap: `internal/runtime/service.go` has zero occurrences of spend; the `db.spend` table has zero writers or readers; provider `Result` (`internal/provider/provider.go:32-35`) captures only `SessionID` and `AssistantOutput` — no usage/token counts, so spend cannot be tracked even if a consumer existed.

**Impact:** A safety-critical, explicitly-declared control is a no-op. A `$5` cap does nothing; a runaway variant will not be stopped. The schema comment describes a consumer that was never built.

**Recommendation:** Implement enforcement end-to-end (add `Usage{InputTokens, OutputTokens, CachedInput}` to `provider.Result`, populate in each runner, accumulate into a spend store keyed by session, check against the cap in the worker loop with hard stop) — or remove the presence-check theater and the `spend` table. Do not leave the middle state.

## HIGH

### H1. Two overlapping SQLite data models: `internal/db`'s task/session/spend schema is orphaned by `internal/plandag`
**Files:** `internal/db/schema/001_initial.sql` (`tasks`, `tasks_fts`, `sessions`, `spend`), `internal/plandag/schema/0001_initial.up.sql`, `internal/db/db.go:229-400`, `internal/runtime/service.go`

Verified consumer counts for the `db` task/session API outside tests: `ClaimTask` 0, `FinishTask` 0, `InsertSession` 0, `MarkSessionExited` 0, all spend methods 0, `ListTasks` 2 (status CLI only), `EnqueueTask` 1 (IPC `enqueue` handler). In `service.go`, `eventDB` is used only for `Close` and `Append`.

**Impact:** Two sources of truth for the same domain concept. The FTS dedup logic, the `spend` table (see C2), and the whole `db` task/session API represent a superseded design left in place.

**Recommendation:** Keep `internal/db` as the pure append-only event log; delete the `tasks`/`tasks_fts`/`sessions`/`spend` tables and their Go API. Move spend-cap accounting into `plandag` or a dedicated store keyed off the plandag session model. Resolves H2 too. (Matches code-quality finding C2.)

### H2. The `enqueue` IPC command writes to the orphaned queue and is disconnected from the scheduler
**Files:** `internal/runtime/handler.go:24-38` (`HandleEnqueue`), `internal/ipc/protocol.go:105-116`, `internal/runtime/service.go` (`dispatchOnce`)

`HandleEnqueue` inserts into the `internal/db` `tasks` table; `dispatchOnce` reads exclusively from the plandag store. Nothing ever reads what `enqueue` writes — anything enqueued via IPC is silently dropped from execution.

**Recommendation:** Either re-point `enqueue` at the plandag store or remove `CmdEnqueue`/`EnqueueArgs`/`EnqueueReply` from the protocol. Given the "Fixit Ralph is the only free-form-ask translator" rule, removal is likely correct.

### H3. IPC protocol has no version field or negotiation despite being a durable, long-lived socket contract
**Files:** `internal/ipc/protocol.go`, `internal/ipc/server.go`, `internal/ipc/client.go`

No `ProtocolVersion` anywhere in `internal/ipc`. `service install` creates a long-running service, so client and server binaries of different versions will routinely coexist across upgrades. A `StatusReply` field rename silently breaks an older client with an opaque JSON-unmarshal failure. The DB layer got a version-skew guard (`plandag.currentSchemaVersion`); the wire protocol did not.

**Recommendation:** Add a `protocol_version` int to `Request` (echo the server's in `status`/handshake). Reject or warn on mismatch with an actionable message.

### H4. `internal/runtime/service.go` is a ~900-line god object owning the entire service lifecycle
**Files:** `internal/runtime/service.go` (902 lines, 32 functions on `Service`)

Owns lifecycle/PID files, config reload + provider-binding validation, scheduler loop, worker start/finish/execute, workspace acquisition, capacity accounting, heartbeating, status projection, event logging, plus free helpers (`resolveObjectStore`/`resolveSyncSource`/`resolveLFSMode`/`chooseProfile`/`defaultEffort`).

**Recommendation:** Decompose into `scheduler` (dispatch loop + capacity), `worker`/`executor` (start/execute/finish/heartbeat), with `Service` as the lifecycle owner. The `resolve*` helpers belong in `internal/config` or `runtime/resolve.go`. Natural home for the unified admission-control gate from C1. (Matches code-quality M6/H4.)

## MEDIUM

### M1. Provider `Result` contract cannot carry usage data, blocking spend enforcement and observability
**Files:** `internal/provider/provider.go:31-35`

`Result` exposes only `SessionID` and `AssistantOutput`. Providers emit token usage in stream-json result events, but the neutral contract discards it. Structural prerequisite for C2.

**Recommendation:** Extend `Result` with an optional `Usage` struct (input/output/cached tokens, optionally cost).

### M2. "MCP tool surface" does not exist in the Go code
**Files:** repo-wide; `docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md:865`

The environment advertises `mcp__radioactive-ralph__*` tools (`plan_claim`, `variant_spawn`, etc.), but there is no MCP server in the Go code: no MCP/jsonrpc library in `go.mod`, no tool registration, no stdio-RPC server. Design docs record: *"MCP server acting as a live bridge (confirmed impossible in Claude Code 2026)."*

**Recommendation:** Reconcile documentation/tooling manifest with reality — strike the phantom MCP surface from operator-facing listings or build it.

### M3. Test-only harness code lives in the production `provider/claudesession` import tree
**Files:** `internal/provider/claudesession/internal/fakeclaude/main.go`, `internal/provider/claudesession/cassette/`

No non-test file in `internal/provider/*.go` references `cassette`/`replayer`/`fakeclaude`.

**Recommendation:** Move the cassette recorder/replayer and `fakeclaude` under a `testdata`/`internal/testsupport` tree or top-level `tools/`; keep `claudesession` focused on the live session runner.

### M4. `plandag/task.go` is a ~925-line single file spanning the full task state machine
**Files:** `internal/plandag/task.go`

**Recommendation:** Split along natural seams: `task_types.go`, `task_claim.go`, `task_transitions.go`, `task_scan.go`. (Matches code-quality M2 territory.)

### M5. `plandag` cycle prevention lives in Go while the DB only has a self-edge CHECK
**Files:** `internal/plandag/schema/0001_initial.up.sql:102-110`, `internal/plandag/task.go:144` (`wouldCreateCycle`)

Any writer bypassing `AddDep` (direct SQL, future code path, `plan import`) can introduce a multi-node cycle the schema accepts.

**Recommendation:** Keep the Go check; document `AddDep` as the only sanctioned edge writer; add a defensive acyclicity re-validation on `plan import`.

## LOW

### L1. Provider defaulting logic duplicated between `ResolveBinding` and `builtInProvider`
**Files:** `internal/provider/provider.go:43-100` — centralize the "resolve name → ProviderFile with defaults" step.

### L2. Windows/Unix service-host split has heavy build-tag surface
**Files:** `internal/ipc/transport_unix.go`/`transport_windows.go`, `cmd/radioactive_ralph/service_windows*.go` (4 variants + stubs) — verify each stub is required; consolidate behind a single interface where possible.

### L3. `db.tasks` timestamp parsing silently swallows parse errors
**Files:** `internal/db/db.go:355-356` — `t.CreatedAt, _ = time.Parse(...)`. Moot if H1 removes the table; otherwise surface the error.

## What is architecturally sound (verified)

- **Dependency direction is clean and acyclic.** `config`/`xdg`/`voice`/`rlog` are leaves; `runtime` sits correctly at the top; `ipc` does not leak into `runtime`. No import cycles.
- **Provider abstraction is right-sized.** `Binding`/`Request`/`Result`/`Runner` is a genuine provider-neutral contract; declarative providers extend it without touching built-ins.
- **Variant profiles are a strong data-driven design.** `Profile.Validate()` fails fast on inconsistent declarations; the `CanMutateViaBash` defense-in-depth reasoning is exemplary — the only gap is that validation is registration-time and the durable path doesn't re-check (C1).
- **plandag migration story is correct.** Embedded migrations in lexical order inside transactions, `user_version` tracked, refuse-if-DB-newer guard — the model the IPC layer should copy (H3).
- **Composite-key task/dep schema is well-modeled** — `PRIMARY KEY (plan_id, id)`, cascade deletes, append-only `task_events` audit split from high-write `task_heartbeats`, updated-at triggers.

Root cause stated plainly: **safety enforcement was implemented in the CLI command layer instead of the shared runtime core, and a superseded data model was left standing next to its replacement.** One admission-control gate in `internal/runtime`, one task/session store in `plandag`, and `internal/db` reduced to the event log it actually is would resolve the majority of the structural debt.
