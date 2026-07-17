# radioactive-ralph — supervisor-architecture rewrite directive

**Status:** ACTIVE — never flips to RELEASED without an explicit manual "end the
session" from the user (directive 2026-07-17). The loop below keeps generating
work; the queue never legitimately drains.

## Directive 0 — the perpetual-shipping loop (ALWAYS ACTIVE, never checked off)

Standing mandate (user, 2026-07-17): "block on nothing. I want YOU making all
calls and working autonomously... always forward exploring with different review
skills and finding new polish and features that make this a better product until
I manually end the session. Until then keep mutating your loop and scheduled
reminder and directives so you're always shipping new PRs."

**This item is NEVER done and NEVER `[x]`.** It is the engine that keeps the
queue non-empty. Each pass:

1. **Ship the current concrete item** (the checklist below) to a merged PR —
   build → self-review trio (code/security/simplify) → PR → babysit CI + bots →
   squash-merge. Full autonomy: open PRs, resolve threads, and self-merge green
   PRs without asking. Design/architecture/packaging/naming calls are MINE; an
   AskUserQuestion for an impl/design detail under this mandate is a defect.
2. **When the concrete checklist is empty, GENERATE the next item** by
   forward-exploring — rotate through these lenses and append what they find as
   fresh `[ ]` items under "Rolling improvement queue":
   - comprehensive-review / feature-dev:code-reviewer on the newest merged code
   - security-scanning:security-sast on any new surface
   - code-simplifier on the largest / newest files
   - UI/UX pass on the TUI + GUI (run them, screenshot, compare to intent)
   - a NEW feature or polish that makes Ralph a better product (GUI richness,
     provider coverage, observability, DX, docs, perf)
   - dependency freshness + CVE sweep
3. **Compress finished work.** When a phase/effort fully lands, move its detail
   OUT of this file into the pillar doc `docs/superpowers/PILLARS.md` (one
   tight paragraph per shipped effort with the merge SHA + PR#), and leave only a
   one-line pointer here. Keeps the directive short and scannable.
4. **Keep the loop alive.** Re-arm ScheduleWakeup every tick; mutate cadence and
   the concrete queue as the work demands. Only a true blocker (interactive
   credential entry, a spend needing payment auth, physical hardware, or
   remote-state-I-already-triggered) is a legitimate `[WAIT-*]` yield — and even
   those route to OTHER queued work rather than halting. The user's SignPath
   enrollment is optional and NOT a blocker: everything ships unsigned-but-
   working without it.

Only the user typing an explicit end ("end the session", "stop the loop", "we're
done") flips Status→RELEASED and stops directive 0. Nothing else does.

---

**Original rewrite status (historical):** rewrite merged (v0.10.0). "Done" = every gap we identified + everything comprehensive-review / UI-review / security / simplification / bug-hunt digging surfaces is resolved. Drove a full multi-lens audit of merged main, then the desktop-app + onboarding effort.

Orchestrator: this agent. Executors: chosen per-task (haiku=mechanical,
sonnet=standard impl, opus/fable=hard reasoning) via Workflow fan-outs.
Each task ends build/test-green (branch is mid-flight but every checkpoint
compiles + passes its own tests). One large branch; final PR(s) at the end.
Full decision trail: .agent-state/decisions.ndjson. Spec:
docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md.

## Shipped (compressed → docs/superpowers/PILLARS.md)

- Supervisor-architecture rewrite (v0.10.0) — PRs #73/#74/#75.
- Post-release multi-lens audit (→ v0.10.3, converged) — PRs #76/#79/#81/#83.
- Guided first-run onboarding — PR #85 (80daad9).
- Versioned IPC drive+observe API — PR #87 (2f20adf).
- Fyne desktop GUI client — PR #89 (e969551).
- Desktop app + packaging + GUI polish (v0.15.0 + since) — native installers #92,
  desktop-app docs #94, GUI macro-view richness #96, deps #97, TUI liveness
  header #98, GUI guide #101, GUI+packaging correctness #102 (P1 AppImage-FUSE
  release-blocker), API-docs regen #104, arch cask #106, GUI Escape-back nav #107,
  directive/PILLARS baseline #108, GUI CI locale-flake fix #110.
- Doctor codex-metering blind-spot check (guidance in Detail, not the dropped
  OK-check Remediate) — PR #112 (v0.18.0).
- Dependabot security sweep: x/image 0.41.0, protobuf 6.33.6 (via semgrep
  1.170.0 lifting the OTel<protobuf-5 ceiling), js-yaml 4.3.0 — PR #114.
- GUI+doctor forward-exploration arc (2-reviewer pass → 6 findings, all shipped):
  focus-first-action + its focus-steal fix #116, directive-sync #117, drive-error
  coordination + nav-token + import-form fix #119, doctor state-dir usability
  check #120, destructive-action confirm dialogs #122, GUI scroll-to-top #123,
  doctor claude-auth ErrNotFound classification #125 (releases v0.19–v0.21).

- Never-block / async-dispatch arc (supervisor/store review → 2 findings +
  cascade): async dispatch so a slow provider turn can't wedge the
  tick/enqueue/reaper — goroutine-per-worker + maxParallel semaphore + shutdown
  drain + baseCtx, plus a running-worker heartbeat (so the reaper doesn't reclaim
  a healthy long turn), persistCtx (results survive shutdown), and fan-out leak
  fixes #127; SQLite pool cap at 4 (not 1 — avoids the single-conn deadlock +
  backup-freeze) #129; per-project spend reservation so a capped provider can't
  overspend under concurrency #131; supervisor concurrent-start test de-flaked
  #132 (releases v0.21.0–v0.21.2).

- Never-block hardening & audit-driven correctness (two opus adversarial audits —
  orchestrator concurrency + store claim-path — drove the fixes): dispatch-turn
  panic containment + immediate claim reclaim, and heartbeat-leak-on-panic fix
  #146; oversized stream-json line FAILS the turn (retryable) instead of masking
  a killed worker as a done step, + process-tree reap so it can't hang #144;
  approval-gate dead-end closed — an approved 'ready' task is now claimable #147;
  ResourceExceeded purged from generated API docs #143; and the store audit's C2,
  which a codex P1 on the follow-up proved a LIVE reaper double-execution bug
  (unheartbeated worker session → step-2 delete → cascade-kill live worker →
  re-dispatch), fixed via worker-session heartbeat + a step-2 session-delete
  guard #149. Dead-raw error-contract cleanup #150 (v0.21.3+). The orchestrator
  audit otherwise gave async dispatch a clean bill (no races/leaks).

Detail lives in PILLARS.md; consult .agent-state/decisions.ndjson for the why
behind any load-bearing call.

## Concrete queue (current)

Kept CURRENT each tick (do NOT commit this file onto feature branches — the
branch-switch churn keeps resurrecting a stale version; this baseline is synced
periodically via a chore/directive-sync PR, of which THIS is one).

(The never-block arc — #127/#129/#131/#132 — is in the Shipped ledger above.
#131 merged; #132 green. This directive-sync PR itself is the current in-flight
concrete item.)

## Rolling improvement queue (directive 0 appends here)

Completed this arc (audits → fixes, all shipped):
- [x] Orchestrator async-dispatch concurrency audit → panic containment #146.
- [x] Store claim-path audit → approval-gate dead-end #147; LIVE reaper
  double-execution bug (C2, codex P1) fixed via worker-session heartbeat +
  step-2 guard #149; dead-raw error-contract cleanup #150.
- [x] Provider-runner audit (opus) → codex nonzero-exit laundering fixed
  (Agent.ExitErr, codex fails on nonzero exit) #152; superviseAgent scopes its
  agent.Watch via a child ctx it cancels on return #153 (reframed honestly — the
  onLine-done leak wasn't reproducible because Kill collapses the window; the
  change is defensive ownership scoping, not a claimed live-leak fix).
- [x] Approval-gate producer → DECIDED to wire it (intended feature, fully
  surfaced, only the producer was missing). A plan step's trailing [approval]
  marker materializes the task as ready_pending_approval; DispatchNext gate-
  checks before spawning worker rows (a bot P2 caught per-tick orphan-row
  accumulation, fixed) #154.

Completed since (all shipped):
- [x] Agent watchdog audit (opus) → #156: Kill no longer SIGKILLs a reaped/
  recycled PID — redesigned (after a codex P1 disproved a mutex approach) to
  route Kill through exec.Cmd's own Cancel→Wait via a private cancelable ctx, so
  a signal can never land after the reap; Watch no longer spurious-stalls on a
  non-positive StallTimeout.
- [x] TUI rendering audit (opus) → #157: cursor follows the selected ENTITY by
  ID across a refresh (not just a clamped index — two codex P2s sharpened this),
  and ALL gather paths route through one in-flight guard so a slow refresh can't
  stack overlapping gathers.
- [x] Verified the app RUNS: `doctor` 11 OK/0 WARN/0 FAIL, all three providers
  detected+authenticated. Cleared 20 stale branch-switch stashes.

- [x] IPC-layer audit (opus) — 5 findings, all MERGED: request read deadline +
  32MiB LimitReader, response/Attach write deadlines, Stop closes all conns
  (skipping the stop-requester so it keeps its reply), proto-version guard #160;
  and the vanished-Attach-client leak — a read-side disconnect watcher cancels
  the handler ctx on EOF #165.
- [x] GUI audit (opus) — clean EXCEPT the single-shot live Attach stream (died
  after the first supervisor blip); runAttach now reconnects in a loop #164
  (merged). Confirmed the TUI'S wrong-entity-action class does NOT exist in the
  GUI (drive buttons capture entities by identity), and thread-safety is sound.
- [x] CI: the GUI-check flake was a go-text/typesetting harfbuzz panic on Fyne's
  bundled font (NOT locale — the first theory was wrong); fixed by FYNE_FONT →
  DejaVu Sans #162 (merged).

Audit sweep COMPLETE across all major subsystems: orchestrator, store,
provider-runners, agent-watchdog, TUI, IPC, GUI.

Structured attach event surface (the observe half goes live) — shipping arc:
- [x] #167 approval-marker operator docs (merged); #168 the event-stream design
  spec (merged; three codex P1s folded in — plan-linked scoping, project id in
  AttachArgs, client-owned cursor to close the backlog↔attach race).
- [x] Self-review of #169 (security + code-review agents) — DONE. Security:
  clean (SQL parameterized, tail loop bounded, cross-project scope correct for
  the single-user local socket). Code-review: two findings — one REAL (Printf
  verbs in two s.log calls where s.log is structured slog → !BADKEY garbage;
  fixed forward, commit 6fceb90) and one FALSE POSITIVE (a stale reviewer clone
  claimed tick_test.go still used the 2-arg HandleAttach; the pushed tree has
  the 3-arg fix, build/test green). The marshal-skip-and-advance tradeoff both
  agents noted is intended (don't wedge the stream on one bad row).
- Attach event stream — the observe half goes live (COMPLETE, compressed →
  docs/superpowers/PILLARS.md): producer #169, consumers #173, json.Valid
  hardening #175, arc compression #174, code-simplifier's one clean change #176
  (collapse pass-through Client.Attach) — all merged. Two review lenses over the
  merged surface came back with no open findings: security-auditor CLEAN (bounded
  resources, parameterized SQL, total input validation, correct scoping — the
  prior #160/#165/#169 fixes closed the real exposure); code-simplifier → #176.

- Events CLI #178 (MERGED): `radioactive_ralph events` tails the project's events
  to stdout (--backlog N, --json) — the observe API's first CLI consumer +
  Client.AttachEvents' first production caller. Review folded 3 findings forward:
  backlog↔live duplicate race (cursor from the SAME read, not a separate
  MaxEventID), --json marshal-drop → stderr notice, and — the notable one —
  ListProjectEvents used a bare project_id filter that SILENTLY DROPPED
  plan-scoped lifecycle events (task.claimed/done/failed) from the CLI backlog
  AND the pre-existing TUI macro pane + GUI event view; fixed by switching it to
  the shared eventProjectScope so all consumers agree with the live tail.

- Session-long TUI live tail (MERGED): spec #180, feature #182 — macro/meso views
  go push-live (subscription starts once on first fetch, routes frames by level;
  poll reconciles via mergeEventTail; id-less-frame dedup + poll-drops-live-event
  bug fixed in review). The observe-half push-live work now spans CLI (#178) /
  TUI (#173/#182) / GUI (#173).

Rolling (next per directive 0):
- [ ] [WAIT] #184 (feat: cursor-aware TUI reconnect) — closes the one real
  limitation the #180/#182 reviews surfaced: the subscription re-seeded from
  MaxEventID on reconnect, missing gap events. Now DataSource.Attach takes an
  afterID; the model tracks lastEventID and resumes from it on reconnect (0 on
  first attach = from now), so no macro event is missed across a supervisor blip.
  +regression test (resume from id 14, not 0). CI; merge green.
- [ ] After #184: rotate a fresh review lens (comprehensive-review /
  code-simplifier) over the merged TUI subscription code, then pick the next
  product feature (live macro plan-PROGRESS deltas; GUI true per-event delta
  apply; or a NEW area — provider coverage, observability, DX) per directive 0.

## Notes

- Model selection for subagents: haiku=mechanical, sonnet=standard, opus/fable=hard reasoning; reserve opus for <10%.
- Per-commit self-review trio (code/security/simplify) then fold findings forward; never amend a reviewed commit.
- CodeRabbit/bot rate-limit red check = false-flag; the signal is the review threads (resolve via GraphQL), not the check status.
