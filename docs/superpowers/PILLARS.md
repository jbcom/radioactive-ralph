# radioactive-ralph — Pillars (shipped-work ledger)

Compressed record of every fully-landed effort — one tight paragraph each with
the merge SHA / PR. The live work queue is `.agent-state/directive.md`; this doc
is where finished directives go so the queue stays short (directive 0, step 3).

## Supervisor-architecture rewrite (v0.10.0)

Replaced the dead variant/kong/plandag model with the supervisor architecture: a
headless supervisor core owning the pty + all store writes, dumb clients on a
Unix-socket/named-pipe IPC, one user-level XDG SQLite DB, accumulated-fingerprint
project identity, goldmark plan decomposition, orchestrator-verified task
completion, and the never-block control invariant (watchdog kills a stalled
agent; the system can never wedge). Phases 1–9: pty-owned agent + watchdog,
store + reaper + backup, cobra/viper config virtual-layers, supervisor +
discovery, providers + capability detection (no personas), plan engine + orch +
A2A vocabulary, read-only Bubble Tea TUI + planning genesis, E2E harness + CI,
and a total docs realignment. Merged via PR #73 (00c788d); API-doc regen + site
realignment #75 (a3532c2); release-please cut v0.10.0 (#74).

## Post-release multi-lens audit (v0.10.0 → v0.10.4)

A convergence audit of merged main: parallel adversarially-verified review
workflows (code/security/simplify/bug-hunt lenses) fixing everything real, then
re-auditing the fixes themselves. Trend 29→14→5→1 confirmed findings across four
passes until essentially-zero reachable defects remained. Landed real bugs incl.
owner-guarded task transitions (MarkFailed/Done/Blocked + ErrTaskNotOwnedRunning),
clock-driven added_at ordering, pty-echo watchdog false-kill (DisableEcho with
per-platform termios), a chmod-TOCTOU socket-dir hardening, and a watchdog
false-kill on claude's own stream-json output. PRs #76 (e8268db), #79 (f8b3de2),
#81 (a75abd3), #83 (eedb6d3); release-please cut v0.10.1–v0.10.3.

## Guided first-run onboarding

A TTY-gated wizard that, on a cold `radioactive_ralph` run (no service, no
supervisor, no DB), OFFERS to set everything up in one consent-gated step:
`service install` (XDG state root + the user SQLite DB + native launchd/systemd/
SCM unit, started), then `--init`, then the TUI. Never prompts on non-TTY/CI
(keeps the print-commands-exit-nonzero path). `internal/onboard` (DI wizard +
StdinPrompter), `service.Start`, client.go wiring, unit tests + a real-pty E2E.
Merged PR #85 (80daad9).

## Versioned IPC drive+observe API

Extended the read-only-TUI IPC into a versioned drive+observe surface so the GUI
can act, not just watch: protocol v2 (`ProtoVersion` + stable `Response.Code`
error classes), an optional `DriveHandler` (plan-import / plan-set-status /
task-approve / worker-kill) the server type-asserts, typed client methods +
`CodedError`/`IsCode`, store `ApproveTask`/`ReclaimWorker`, and `plan import`
routing through the supervisor as the single writer. Second-scrutiny review
(gemini/codex/Amazon-Q) fixed real bugs: pause actually pauses (dispatch filters
to active-only), worker-kill cancels the live provider process (orch
cancellation registry + `KillWorker`), `ReclaimWorker` requeues ALL a fan-out
worker's tasks by `claimed_by_worker_id` without stomping a reassigned one or
penalizing the retry budget, a proto-version guard before dispatch, atomic
`ApproveTask`, and a typed `ErrPlanNotFound`. Merged PR #87 (2f20adf).

## Fyne desktop GUI client

A Go-native (Fyne) desktop app: a peer to the TUI on the same supervisor socket
that watches AND drives (approve/pause/resume/abandon/kill/import). CONSISTENT
identity — a `ralphTheme` + a shared `internal/statusbucket` palette the TUI was
refactored to consume too (one source, anti-drift test), and the same
macro→meso→micro drill. A read+drive `Controller` seam; `liveController` forwards
reads to the store + a fresh short-lived `ipc.Client` and drives via the v2
methods; all IPC runs off the Fyne main thread (gather→snapshot→render,
mutex-guarded selection, -race clean). The whole Fyne dependency is isolated
behind a `//go:build gui` tag (Fyne is CGO-only and would break CI's CGO-off
six-way cross-build) with a `!gui` cobra stub and a dedicated CGO-on GL/Wayland
CI job. Two bot-review rounds fixed two real pre-existing bugs the GUI surfaced
(always-zero status counters → `store.StatusCounts`; fan-out kill only on the
first task → keyed on `claimed_by_worker_id`) plus tray Quit, ctx-cancel
teardown, async launch, local-time, and rune-safe truncation. Merged PR #89
(e969551).

## Native installers & GUI desktop packaging

Ships radioactive-ralph as real installable software everywhere, CLI and GUI,
signed the OSS way (no paid Apple/Microsoft credentials). CLI: goreleaser nfpms
(`.deb`/`.rpm`), a winget publisher, and the Homebrew formula migrated to
`homebrew_casks` (goreleaser v2.17 removed `brews`). GUI: a per-OS `gui-bundles`
release matrix runs `fyne package --tags gui` (CGO) on native runners —
macOS ad-hoc-`codesign`s the `.app`, wraps a `.dmg`, and publishes a Homebrew
cask whose `postflight` strips the quarantine attribute (so the ad-hoc-signed
app opens without a Gatekeeper prompt, no Apple Developer account); Linux repacks
fyne's tarball into an AppImage (appimagetool pinned + SHA-verified) with the
committed `.desktop`; Windows produces the `.exe` with an optional secret-gated
SignPath Foundation Authenticode stage (free OSS signing when enrolled). A
double-clicked bundle (no controlling TTY) launches the GUI via a build-tagged
hook rather than the bare TUI. A `packaging` CI job (goreleaser check +
shellcheck + `desktop-file-validate`) gates it all on every PR. Four bot-review
rounds hardened the scripts (token out of clone URLs, cleanup traps, rerun-safe
cask push, per-bundle checksums) and fixed two P1s the review caught: the
double-click-opens-TUI bug and the wrong quarantine assumption. Merged PR #92
(a1df782). The one optional follow-up is the user's free SignPath enrollment,
which flips the Windows `.exe` from unsigned to signed with no code change.

## Desktop-app polish arc (post-packaging)

A perpetual-shipping run of small, self-reviewed PRs hardening and documenting
the desktop app. User-facing docs for the GUI + desktop installs (#94, 0716a92);
a GUI macro-view "Recent activity" project-events feed + a live "connected · up
<dur>"/"waiting for supervisor"/"disconnected" status header + a non-mutating
desktop-launch project resolution + a RenderToMarkup visual-regression test
(#96); the same liveness header backported to the TUI (#98); a dedicated GUI
guide page (#101); a comprehensive-review-driven correctness batch — the P1
AppImage-FUSE release-blocker (APPIMAGE_EXTRACT_AND_RUN), import hidden in
project-agnostic mode, a refreshNow stale-paint seq-guard, "all projects" count
labeling (#102); an API-docs regen adding the GUI-tagged surface + dropping
dead-model index text (#104); a two-arch macOS cask so Intel Macs get a working
install, incl. the Bash-3.2 associative-array fix (#106); and Escape-to-drill-
back GUI keyboard navigation, focus-safe via the desktop KeyDown hook (#107).
Each PR absorbed its bot + CI review before merge; captured in release v0.15.0
and the ones after.

## Perpetual-shipping run (post-v0.15.0)

The autonomous loop (directive 0) continued shipping small, self-reviewed PRs.
A directive + PILLARS baseline sync so branch-switch churn stops resurrecting a
stale queue (#108); a GUI CI locale-flake fix pinning `LANG`/`LC_ALL=en-US`
because a bare `C`/`C.UTF-8` locale made Fyne's harfbuzz shaper panic on an
undefined language tag, verified locally (#110); a doctor DX check that surfaces
the codex spend-cap metering blind spot — codex has no machine-readable usage
stream so its cost isn't metered and a spend cap can't be enforced, with the
account-level mitigation carried in the check's `Detail` (not `Remediate`, which
`WriteText` drops for OK-severity checks) — v0.18.0 (#112); and a Dependabot
security sweep clearing all four open alerts: `golang.org/x/image` 0.24.0→0.41.0
(TIFF PackBits/OOM DoS), `protobuf` 4.25.9→6.33.6 (CVE-2026-0994, unblocked by
bumping semgrep 1.136.0→1.170.0 to lift its pinned `opentelemetry-proto<protobuf-5`
ceiling), and `js-yaml` 4.1.1→4.3.0 (merge-key quadratic DoS) (#114). Each PR
absorbed its bot + CI review and self-merged green.

## GUI + doctor forward-exploration arc (v0.19–v0.21)

A two-reviewer multi-lens pass over the merged GUI + doctor surface surfaced six
real findings, each shipped as its own self-reviewed PR. GUI: focus the first
actionable control on each drill so keyboard users don't blind-Tab — gated on the
view-identity change so the 1s tick doesn't yank focus mid-Tab (#116); coordinate
drive-action errors with the paint loop (a failed Approve/Pause/Kill banner was
silently erased by the next refresh) plus a viewToken so an in-flight action that
completes after the operator navigates away can't resurrect a banner, and an
importing flag so the periodic paint stops wiping the imperative Import form
mid-edit (#119); modal confirmations on the two irreversible one-click actions
(Abandon plan, Kill worker, with the worker id in the prompt), wording corrected
to match the supervisor's real HandlePlanSetStatus semantics — running tasks
finish, and it's resumable (#122); scroll-to-top on drill (#123). Doctor:
checkStateDir verifies the XDG state root is resolvable/writable (exclusive
CreateTemp probe) with a low-disk WARN, catching full-disk/wrong-perms installs
that previously passed doctor then threw a cryptic SQLite error at first run, with
a per-platform diskFreeBytes helper guarded against gosec G115 (#120); and
checkClaudeAuth distinguishes a missing CLI from an unauthenticated one, mirroring
checkCodexAuth's ErrNotFound branch (#125). Directive/PILLARS reconciled to main
via #117. Releases v0.19.0–v0.21.0.

## Never-block / async-dispatch (v0.21.0–v0.21.2)

A supervisor/store review found the central never-block invariant violated on the
hottest path: dispatchWorker ran the provider agent turn (up to the 5-min
StallTimeout) INLINE under the supervisor's dispatchMu, so a slow turn wedged the
periodic tick, every HandleEnqueue client, and the reaper — while the DispatchNext
doc comment already (falsely) promised goroutine-per-dispatch. The fix (#127)
moves the slow turn+verify into a goroutine (per-step and native-fanout paths)
behind a maxParallel try-acquire semaphore, with a WaitGroup the supervisor drains
on shutdown; the goroutines run under a base context (the supervisor run ctx) not
the per-request IPC ctx that dies when the request returns. Careful review
reception then caught and fixed several edge cases the async change exposed: a
running-worker store heartbeat (every 20s) so the reaper doesn't reclaim a healthy
turn longer than its 90s staleness window; a persistCtx (detached from shutdown
cancellation) so a nearly-complete turn's evidence + verification still land; and
two fan-out task-leak paths. Supporting fixes: the SQLite pool is capped at 4
connections (#129) — not 1, which would deadlock database/sql and let a backup's
VACUUM INTO freeze the whole supervisor — bounding writer-lock contention while
leaving headroom for a live backup + WAL readers; the spend-cap check reserves an
in-flight slot per (project, provider) so concurrent launches on a capped provider
overshoot by at most one turn (#131); and the supervisor concurrent-start test was
de-flaked to bounded, race-independent assertions (#132).

## Never-block hardening & audit-driven correctness (v0.21.3+)

With async dispatch landed, adversarial audits (opus concurrency review of the
orchestrator; opus SQLite/claim-path review of the store) drove a round of
resilience + correctness fixes. **Resilience:** the async per-step and fan-out
dispatch goroutines had no panic recovery, so a panic in a worker turn or its
verification crashed the whole supervisor — a direct never-die violation.
recoverDispatchPanic (installed as the LAST defer so it runs FIRST during
unwinding, before the inflight.Done() that unblocks Wait(), guaranteeing its
writes land while the store is still live) now logs a worker.dispatch_panic
event AND reclaims the goroutine's claimed task(s) via ReleaseClaim (back to
'pending' with no retry-budget penalty, since a panic is orchestrator-level, not
task-level) and clears the worker row, so a panicked turn is immediately
re-dispatchable instead of wedged until the 90s reaper; runWithHeartbeat's
heartbeat reap was also moved into a defer so a panicking turn no longer leaks
its heartbeat goroutine (#146). **Provider correctness:** a stream-json frame
over the 16MiB scanner cap previously discarded the whole turn; the first cut
salvaged the pre-oversize frames as a SUCCESSFUL turn, but that fed partial text
into Evidence.Output where the judgment-only acceptance check (non-empty output
⇒ done) could mark a step complete on a killed-mid-stream worker — so the final
form FAILS the turn with ErrStreamJSONLineTooLong (retryable) and keeps NO
partial text in AssistantOutput, so a killed turn can never satisfy completion,
and reaps the process tree before cmd.Wait() so a CLI still writing the oversized
line into a full pipe can't hang the turn (#144). **Store correctness:** ApproveTask moved a gated task to
'ready', but the claim path (Ready, ClaimNextReady, its atomic UPDATE) matched
only 'pending' and nothing bridged 'ready'→'pending', so an approved task was
stranded forever; all three sites now accept status IN ('pending','ready') while
still excluding ready_pending_approval, making the already-wired Approve button
safe the moment a gate producer lands (#147). Housekeeping: the removed
ResourceExceeded watchdog signal was purged from the generated API docs and its
test renamed (#143).

## Runner audit + store-reaper fix + approval-gate producer (v0.21.4+)

A second wave of adversarial audits (opus store claim-path + opus pty-runner)
plus the approval-gate wiring. **Store reaper (live bug):** a worker's OWN
session row was heartbeated by nothing — runWithHeartbeat beat only the worker
row — so a provider turn longer than the reaper's session-delete window
(staleSessionMultiplier * staleAfter = 270s) let step-2 delete the stale session,
CASCADE-kill the still-live worker, NULL its running task's claim, and re-dispatch
that task to a second worker (double execution). Caught by a codex P1 on a
follow-up after my first pass wrongly called it latent. Fixed by beating the
worker session in lockstep (HeartbeatWorkerAndSession) AND excluding sessions
with a fresh worker from step-2 deletion (#149). **codex runner:** codex has no
structured terminal frame, so superviseAgent returned nil on ANY exit — a
nonzero exit (auth/model error, crash) that had written a partial
--output-last-message was laundered into a successful, zero-cost result,
defeating both correctness and spend accounting. The agent layer now captures
cmd.Wait()'s status (Agent.ExitErr, nil when killed) and codex fails the turn on
a nonzero exit (#152). **Watchdog scoping:** superviseAgent now runs agent.Watch
under a child ctx it cancels on return, bounding Watch's lifetime to its own
scope rather than the caller's — defensive ownership (the onLine-done leak the
audit hypothesized wasn't reproducible because Kill collapses the window) (#153).
**Approval-gate producer:** the whole approval surface existed (GUI Approve
button, IPC/statusbucket, ApproveTask) with no producer; a plan step's trailing
[approval] marker now materializes the task as ready_pending_approval, and
DispatchNext gate-checks each step before spawning worker rows so a pending gate
doesn't accumulate orphan session/worker rows per tick (#147 made 'ready'
claimable; #154 wired the producer). Plus the dead-raw error-contract cleanup
(#150) and the raw-error diagnostics polish. Runner-audit clean on frame
parsing, usage accounting, kill correctness, and stall handling.

## Agent-watchdog + IPC + GUI audits (v0.21.5+)

The remaining three subsystems audited, completing an adversarial-audit sweep of
the whole runtime (orchestrator, store, provider-runners, agent-watchdog, TUI,
IPC, GUI). **Agent-watchdog:** Kill could SIGKILL an already-reaped/kernel-
recycled PID (it decided via state that only reflects after cmd.Wait RETURNS,
but Process.Wait reaps inside it) — a codex P1 disproved a mutex attempt, so the
fix routes Kill through exec.Cmd's own Cancel→Wait via a private cancelable ctx,
which never signals after the reap; and Watch no longer spurious-stalls on a
non-positive StallTimeout (#156). **IPC:** the server had NO read/write deadlines
and NO request size cap — a bad client could hang shutdown, leak goroutines/fds,
or OOM the supervisor; fixed with a bounded request read (deadline + 32MiB
LimitReader), response/Attach write deadlines, Stop closing all conns so shutdown
drains promptly (skipping the stop-requester so it still gets its reply), and a
proto-version guard before dispatch (#160); plus a read-side watcher that detects
a vanished Attach client (EOF) and cancels its handler so it doesn't leak (#165).
**GUI:** clean except the live Attach stream was single-shot — it died
permanently after the first supervisor blip; runAttach now reconnects in a loop
(#164). The GUI audit confirmed the TUI's wrong-entity-action class does NOT
exist here (drive buttons capture entities by identity) and thread-safety is
sound (all widget mutation on the Fyne main thread via fyne.Do). **CI:** the
GUI-check flake was a go-text/typesetting harfbuzz panic on Fyne's bundled font
(not locale — the first hypothesis was wrong); fixed via FYNE_FONT → DejaVu Sans
(#162). The TUI audit earlier drove cursor identity-reconciliation + a refresh
in-flight guard (#157).

## Attach event stream — the observe half goes live (v0.21.6+)

The IPC drive+observe API had a live *drive* half and a STUB *observe* half: the
supervisor's `HandleAttach` blocked on `ctx.Done()` and emitted nothing, so an
Attach client connected and saw silence, while a rich append-only `events` table
(monotonic autoincrement id, ~20 lifecycle emit sites) was written on every
load-bearing transition and never read (`ListProjectEvents` had zero callers).
The clients rendered by polling. This arc wired the producer to the
already-hardened Attach transport (#160/#165) and made the read-only clients
push-live.

**Producer (#169).** The supervisor TAILS the events table — the single ordered
source of truth — reusing it with zero change to any emit site (rejected an
in-process pub/sub bus and a SQLite update-hook as premature/fragile). New store
tail queries `EventsAfter(projectID, afterID, limit)` (rows with `id > afterID`,
ascending, capped) and `MaxEventID` back a 250ms tail loop in `HandleAttach`. New
`ipc.AttachArgs{ProjectID, AfterID}` + `ipc.AttachEvent` (the public, versioned
wire shape; payload passed through verbatim so a new kind needs no transport
change) + typed `Client.AttachEvents`. Three codex P1s on the spec were folded in
before code: (1) scope by plan-linkage, not bare `project_id` — the headline task
events carry only `plan_id`, so a `project_id`-only filter would drop exactly what
a live view needs; an explicit project_id wins over plan linkage; (2) the project
id travels in AttachArgs (the connection is project-agnostic); (3) a fully
client-owned cursor (no server seed-to-max) closes the backlog↔attach lost-event
race. Review hardening: transient (SQLITE_BUSY/LOCKED) errors retry, permanent
ones (closed/corrupt DB) end the stream instead of spinning; both live clients
seed the cursor to MaxEventID so launch/reconnect starts from "now", not a
full-history replay; structured slog key/values (not Printf) in the tail logs.

**Consumers (#173).** The TUI decodes each frame as `ipc.AttachEvent`, FILTERS the
micro-view tail to the selected task (the deferred codex P2 — a frame for another
task no longer pollutes the drilled-in log), and applies task-status deltas
(`applyEvent`/`taskDeltaStatus`, including `task.blocked`/`task.context_requested`
→ Blocked, a code-review finding) so a lifecycle change lands immediately; the
GUI gates its per-frame `refreshNow` on the event kind (`eventTriggersRefresh`) to
kill the heartbeat-refresh storm. The periodic poll stays the reconcile net —
deltas are the fast path, never the only path. **Hardening (#175):**
`store.jsonOrEmptyObject` now wraps a malformed input as `{"raw":...}` so the
`payload_json` column's "always valid JSON" invariant is structural rather than
caller-discipline (a #169 security-self-review finding). Design specs:
`docs/superpowers/specs/2026-07-17-attach-event-stream-design.md` and
`…-attach-live-consumers-design.md`.

## TUI/CLI observe surface goes push-live (v0.22+)

The observe half of the drive+observe API became a real live feed end-to-end.
**CLI (#178):** `radioactive_ralph events` tails the project's events to stdout
(`--backlog N`, `--json`) — the first CLI consumer of `Client.AttachEvents`;
review folded in a backlog↔live cursor-race fix (cursor from the same read, not
a separate MaxEventID), a `--json` marshal-drop→stderr notice, and — notably —
`ListProjectEvents` was switched to the shared `eventProjectScope` because a bare
`project_id` filter had silently dropped the plan-scoped lifecycle events
(`task.claimed/done/failed`) from the CLI backlog AND the pre-existing TUI macro
pane + GUI event view. **TUI (#182):** the live Attach subscription became
session-long (started once on first fetch, routed by drill level) so the
macro/meso views update from events as they land, not just on the 1s poll —
always applying the lifecycle status delta + a live macro event tail
(`prependEvent`, id-deduped), the per-task filtered log at micro; the poll
reconciles via `mergeEventTail` (a wholesale replace would drop a live event
whose DB commit landed inside the poll's read window — a real bug caught in
review). **Cursor-aware reconnect (#184):** the model now OWNS the resume cursor
end-to-end — it seeds `lastEventID` from `MaxEventID` once before the first
attach (`attachSeeded`) and resumes from it on reconnect (threaded into
`AttachArgs.AfterID`), so no macro event is missed across a supervisor blip even
if the first subscription ended before yielding a frame (a codex P1). Two
post-merge review lenses came back clean: security-auditor (bounded resources,
parameterized SQL, correct scoping) and code-simplifier (one stale-comment
delete). A `govulncheck` sweep found 0 CVEs with all direct deps current. Specs:
`…-events-cli-design.md`, `…-tui-macro-live-events-design.md`.
