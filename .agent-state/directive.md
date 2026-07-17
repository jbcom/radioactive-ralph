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

Completed this arc:
- [x] Orchestrator async-dispatch concurrency audit (opus) — clean on races/
  leaks/semaphore/WaitGroup/ctx/deadlock; surfaced the panic-crash gap → #146.
- [x] Store claim-path/SQLite audit (opus) — core claim path race-safe; found
  the approval-gate dead-end (C1 → #147) and, via a codex P1 on the follow-up,
  a LIVE reaper double-execution bug (C2 → #149): a worker's own session was
  never heartbeated, so a turn > 270s let the reaper delete the session, cascade-
  kill the live worker, and re-dispatch its running task. Fixed by beating the
  worker session (HeartbeatWorkerAndSession) + guarding step-2 session-delete.
- [x] Dead `raw` return on the oversized-line path → #150 (in flight): raw is now
  empty on every error path of runStreamJSONCommand (contract: valid only on
  success), and the ErrStreamJSONLineTooLong doc corrected to match.

Next forward-exploration items:
- [ ] [WAIT] #150 (declarative raw-error contract) — CI running; merge when green.
- [ ] [WAIT-AGENT] Provider-runner audit (opus) — adversarial review of the
  pty-backed claude/codex/opencode runners + watchdog enforcement for misparse,
  control-invariant holes, hangs, kill correctness, leaks, usage accounting.
  Re-invokes on completion; fold confirmed findings into fresh items + ship.
- [ ] Approval-gate producer: nothing yet sets ready_pending_approval, so the
  now-safe Approve button has no live trigger. Decide whether the gate is a real
  product feature (wire a producer — e.g. plan-level approval flag) or dead
  surface to remove (YAGNI). Architecture call for the agent.
- [ ] After the runner audit: rotate the review lens to the agent watchdog / TUI,
  or a NEW product-improving feature (GUI richness, observability, DX).

## Notes

- Model selection for subagents: haiku=mechanical, sonnet=standard, opus/fable=hard reasoning; reserve opus for <10%.
- Per-commit self-review trio (code/security/simplify) then fold findings forward; never amend a reviewed commit.
- CodeRabbit/bot rate-limit red check = false-flag; the signal is the review threads (resolve via GraphQL), not the check status.
