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

## Async dispatch — never-block invariant restored (in flight)

A supervisor/store review found the central never-block invariant violated on the
hottest path: dispatchWorker ran the provider agent turn (up to the 5-min
StallTimeout) inline under the supervisor's dispatchMu, so a slow turn wedged the
periodic tick, every HandleEnqueue client, and the reaper — while the DispatchNext
doc comment already (falsely) promised goroutine-per-dispatch. The fix moves the
slow turn+verify into a goroutine (per-step and native-fanout paths) behind a
maxParallel try-acquire semaphore, with a WaitGroup the supervisor drains on
shutdown after cancelling the run context; the goroutines run under a base context
(the supervisor run ctx) rather than the per-request IPC ctx that dies when the
request returns. A test proves DispatchNext returns promptly while a provider turn
blocks. Design: docs/superpowers/specs/2026-07-17-async-dispatch-never-block-design.md.
PR #127.
