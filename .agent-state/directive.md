# radioactive-ralph — supervisor-architecture rewrite directive

**Status:** ACTIVE — re-armed 2026-07-26 on an explicit user go: orchestrate the
backlog, dogfood Ralph on itself, and finish reconciling what other repo agents
contributed. Landing/reconciliation runs through Claude Code workflows with
per-agent model selection, NOT through Ralph (user, 2026-07-26: "you dont need
ralph for landing all of this and reconciling it"). Dogfooding is a goal, gated
on #205 + #204 landing, not the mechanism for getting there.

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

Worktree/branch reconciliation is DONE (2026-07-26): 18 worktrees -> 5, 17 local
branches -> 5, remote heads -> 2. Method note for future passes: `git cherry`
and three-dot diffs BOTH give false positives here (squash-merge changes
patch-ids; branch-behind-main inflates diffs). The decisive test is
`git merge-tree --write-tree origin/main <branch>` compared against main's tree
— a no-op merge proves full absorption. Never-reviewed branches were archived as
tags before deletion: `archive/plan-v2-dag`, `archive/release-022-recovery-scratch`,
`archive/release-v0220-manual-recovery` (all pushed to github).

- [x] PR #208 admission token boundary — CodeRabbit was right and my first
      counter was wrong: `persist-credentials: false` does NOT scope a token to a
      step; GITHUB_TOKEN stays available to every step via
      `secrets.GITHUB_TOKEN`/`github.token`, so job-level `contents: write` was a
      job-wide write capability and the "only step receives the write token"
      comment was false. Fixed by REMOVING the capability rather than relocating
      it (the reviewer proposed a second write job): built-in token drops to
      `contents: read` and the draft read uses the existing CI_GITHUB_TOKEN. Also
      replaced the admission command blocklist with a structural allowlist —
      `gh api -f x=1 --method PATCH` evaded the old literal `gh api --method`
      check; proven by a negative test. Both threads resolved.
- [x] PR #209 (issue #205) — MERGED as 3045d07; issue #205 auto-closed. Turn
      deadlines separated from stall detection + Darwin process-tree containment.
      The branch was based on v0.22.0 and would have DELETED PR #206's three
      release-authority files and reverted the released 0.22.1 CHANGELOG heading;
      rebased so the diff is purely the fix. Three further defects found and
      fixed during review: (1) the cleanup path guarded on
      `errors.Is(err, syscall.ESRCH)` but `auditTokenForPID` never wraps ESRCH
      (`task_name_for_pid` reports a dead PID as KERN_FAILURE / Mach code 5), so
      the guard could never fire and an ordinary teardown race failed the whole
      cleanup — the exact false-cleanup failure #205 exists to eliminate, fixed
      via an `errDarwinProcessGone` sentinel; (2) unbounded stdout/stderr and
      stream-JSON aggregate sinks, which the 30m turn default widened into an OOM
      path since every write renews the stall lease — now bounded inline at
      16MiB, verified 0.34s vs a 90s timeout without the ceiling; (3)
      `layeredTimeoutValue` accepted any non-empty string, so `turn_timeout =
      "banana"` let dispatch claim a task that then looped forever until stale
      reclamation — now validated at binding resolution via
      `ValidateConfiguredTimeout`.
- [x] PR #212 (DAG increment 1) — MERGED as 72b75b5. Two pre-existing races
      proven on main and fixed: `journal_mode` as a DSN `_pragma` re-ran a
      lock-taking pragma on every pooled connection, and `Migrate` read the
      schema version outside any lock so concurrent first-openers all ran the
      same DDL. Review then caught a bug the fix introduced — the early return
      swallowed the newer-schema case, letting an old binary treat a newer schema
      as already-applied — fixed with `ErrSchemaNewerThanBinary` checked inside
      the transaction, plus cancellation-aware retries. A Windows CI failure I had
      first misattributed to a pre-existing flake turned out to be mine: the retry
      loops stacked on the DSN's own `busy_timeout`, so one contended Open could
      burn >10s of backoff against a 15s test bound. Retries are now a 3-attempt
      backstop (240ms/Open, 44x reduction).
- [x] PR #210 — MERGED. Directive re-armed + the DAG integration spec landed.
- [x] DAG increment 4 (#217) — MERGED, released v0.23.0.
- [ ] [WAIT] Eight PRs open. **#215 (increment 2) MERGED.** Chain:
      **#216 -> #221 -> #225 -> #228**, plus independents #220, #222, #224, #226.
      Reconciliation lesson (2026-07-27): after a stacked parent squash-merges,
      REBUILD the child from main and cherry-pick only its unique commits —
      never rebase it. Rebasing #220 replayed all eight of #219's commits against
      the squash of themselves; `git merge-base --is-ancestor <parent-head>
      <child-head>` proved exactly two commits were unique, and reset+cherry-pick
      applied them with zero conflicts. The squash rewrites the parent's history
      into one commit the child no longer shares ancestry with, so git cannot
      tell the duplicates apart. Same treatment rebuilt #215/#216/#221/#225.
      - #224 fixes a REAL Windows deadlock, not a flake: winio's Accept waits on
        an internal goroutine with no cancellation case while that goroutine
        selects between close and accept; when close loses the coin flip it calls
        ConnectNamedPipe for a client that never arrives and Close never returns.
        Hit TWICE in one afternoon (PR #221 and #225 runs), both
        TestAcquire_SecondFailsWhileFirstHolds, both parked in
        win32PipeListener.Close. Stop now retires the accept loop before closing
        the listener. Cannot be reproduced on darwin, so the Windows CI leg is
        the real check.
      - #226 fixes a genuine CI flake masquerading as infrastructure:
        `$TMP/case-$RANDOM` across five setup_remote calls, $RANDOM drawing
        0..32767 WITH replacement, so a collision makes `git remote add origin`
        fail with "remote origin already exists". Pinning CASE reproduces the CI
        error byte for byte — that match is what identified the cause rather than
        a story that merely fits. ~1 run in 3000.

- [ ] [WAIT-USER] Remote naming is backwards and needs the user's call before I
      touch it. VERIFIED 2026-07-27: every push this session went to `github`
      (git@github.com:jbcom/radioactive-ralph.git) — correct. Gitea is mirroring
      fine: `main` byte-identical on both (20c68f04) and the branch SETS match
      exactly. But `origin` points at Gitea
      (gitea@git.local.jonbogaty.com:jbcom/radioactive-ralph.git), so the
      DEFAULT target of an unqualified `git push`/`git pull`/`gh` is the MIRROR,
      not the source of truth. I have been naming `github` explicitly on every
      command; anything that falls back to the default writes to the wrong
      place. The two branches showing "diverged" (feat/dispatch-walks-graph,
      feat/dumb-client-store-boundary) are just branches I force-pushed today
      with Gitea still holding the pre-rewrite SHAs — mirror lag on rewritten
      history, no content lost, no conflict.
      Proposed: rename so the safe target is the default — `origin`→GitHub,
      `gitea`→the mirror. Blocked on two answers, both outward-facing:
      (1) is Gitea PULLING from GitHub, or does something push Gitea→GitHub?
      No mirror workflow exists in .github/workflows/, which suggests a
      Gitea-side pull mirror — but if the flow is reversed, renaming is not
      enough. (2) rename in all 8 worktrees or only the primary checkout?

- [ ] Issue #204 remainder — after #219 + #220 land, three criteria are left:
      1. `init_cmd.go` is the last direct store user. Beyond project
         resolve/create it pulls in ~90 lines of `vconfig` layer resolution
         (`DiffConflicts`, `EffectiveProject`, `ApplyProjectConfig`), so it
         needs a config-apply command surface of its own — larger than the
         project-ensure command #220 adds. AGENTS.md already settles the design
         question: the client "initializes project config" over the socket and
         "refuses to run without a supervisor".
      2. DONE (PR #231). All three had ZERO production consumers — a grep
         across internal/ and cmd/ found only tests. RepoPath and
         ProviderSessionID were never populated at all. They describe the
         supervisor HOST (absolute path, OS pid), not the work, and went to
         every attached client on every poll. Removed rather than left: a field
         nothing populates and nothing reads invites a future implementer to
         fill it in, which is exactly how RepoPath came to be declared. The four
         referencing tests were about round-tripping and liveness, not these
         fields; ActiveWorkers and ProtoVersion carry those properties now.
      3. DONE (PR #230). Claude failure classification shipped, keyed on the
         result frame's api_error_status. The real-CLI capture REFUTED the
         assumed shape: a rejected key emits
         {"is_error":true,"subtype":"success","api_error_status":401}, so
         subtype says SUCCESS on a hard failure and subtype-keyed logic reads
         it as a completed turn. Categories reach the durable surface
         (provider_auth non-retryable, provider_throttled, provider_unavailable)
         so classification changes what an operator sees, not just an internal
         error. Unrecognized statuses stay generic on purpose.
      Note for whoever picks this up: `ensureProjectKnown` must stay AFTER
      supervisor discovery in client.go. Moving it earlier breaks the first-run
      wizard, which exists for the operator who has no supervisor yet —
      `TestE2E_FirstRunWizardDeclinePath` is the guard.
- [ ] DAG integration — increments 8-12 remain. Central verified finding stands:
      **main already had the DAG store layer** (`task_deps` in 0001_initial,
      cycle prevention in `AddDep`, `Ready`/`ClaimNextReady` already walking the
      edges). The gap was confined to the orchestrator. Shipped/open:
      increment 1 = #212 (WAL + migration races, merged); 2 = #215 (schema 0003 +
      task_metadata, MERGED); 3 = #216 (`ClaimTask`); 4 = #217 (plan model learns
      edges, merged); 5 = #221 (`CreatePlanGraph` + `ImportPlan`, the keystone);
      6 = #225 (dispatch walks the graph); 7 = #228 (CWE-22 path containment).
      Increment 6's real lesson: partitioning the ready wave by persisted
      `group_path` is load-bearing, because native fan-out delegates a whole
      partition to ONE provider under one heading and binding — deciding to fan
      out from `len(ready) > 1` would hand one worker tasks from unrelated
      groups. It also surfaced a compatibility gap the spec had not anticipated:
      sourcing readiness from `task_deps` made `ImportPlan` a PRECONDITION, so
      any plan written by `CreatePlan` directly (or predating the graph) silently
      never dispatched — no error, no event. Dispatch now materializes the graph
      on first sight, which the E2E test caught.
      Increment 7's scope limit is stated rather than implied, per Codex's
      accepted P1 on #210: declared-path containment is best-effort VALIDATION,
      not a write-side boundary. It constrains Ralph's reads and what Ralph will
      dispatch; it does NOT constrain the provider, a separate process opening by
      pathname minutes later. Ralph's guarantee is DETECTION at completion. A
      real write-side guarantee needs a sandbox/namespace/brokered-FS primitive
      around the provider process — separate work, filed as follow-up.
      Remaining: 8 (output reservations), 9 (provider invocation + capabilities),
      10 (calibration), 11 (IPC + clients), 12. Full plan in
      `docs/superpowers/specs/2026-07-26-dag-integration-design.md`.
      Hard discards (unchanged): the source branch's `CreatePlanGraph`
      (duplicated CreatePlan+CreateTask+AddDep AND bypassed the cycle check),
      `graph_validate.go`, `enrichTaskMetadata` + the `Task` widening, `Plan.V2`
      and every `parsed.V2` fork, the Codex arg expansion, and the double
      filesystem verification in `VerifyAndComplete`.
- [ ] Deferred provider CLI flag decisions, own PR with real-CLI verification:
      claude `--permission-mode bypassPermissions` (a security posture change —
      arguable since claude.go:122 already treats a permission prompt as a KILL,
      but it needs its own decision, not a DAG ride-along), claude `--no-chrome`,
      opencode `--pure --auto`. DISCARDED separately: the `defaultOpencodeProvider`
      NativeFanout true->false flip, which regresses a capability main documents
      as verified against installed opencode 1.18.3.
- [x] Windows shutdown flake in `TestRun_SecondRunRefuses`
      (supervisor_test.go:133). Pre-existing — PR #209 changed only COMMENTS in
      internal/supervisor/supervisor.go, and the file's own comments at :17/:153
      document this named-pipe timing flake while :158 calls this very test the
      "deterministic" alternative, which the failure disproves. RESOLVED: my
      initial "pre-existing flake" call was WRONG — evidence refuted it (main
      green, PR #209 zero failures across 10 runs, both failures on the one PR
      touching store.Open). Root cause was mine: retry loops stacked on the DSN's
      busy_timeout(5000), ~10.5s of backoff against a 15s bound. Fixed in #212 to
      a 3-attempt/20ms backstop (240ms per Open, 44x reduction). The SEPARATE
      Windows hang in TestAcquire_SecondFailsWhileFirstHolds is the winio
      deadlock, fixed by #224.

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

- TUI/CLI observe surface goes push-live (COMPLETE, compressed →
  docs/superpowers/PILLARS.md): events CLI #178, session-long TUI live tail
  #182, cursor-aware reconnect #184 (all merged; the reconnect gap is closed —
  the model owns the cursor end-to-end). Two post-merge review lenses clean
  (security-auditor, code-simplifier — one stale-comment delete folded into
  #184); govulncheck 0 CVEs, direct deps current.

- [x] Live macro plan-PROGRESS deltas (#188, MERGED): a live done frame bumps the
  plan's Done counter immediately, closing the last poll-only gap in the macro
  view. Review folded forward: dedup the two completion aliases (worker.completed
  + worker.verified_done) by task-id; take max(live, poll) on the poll merge so
  an in-flight poll can't regress a live bump.

## Session end (2026-07-17)

The TUI/CLI observe surface is fully push-live (compressed → PILLARS.md): events
CLI #178, session-long TUI tail #182, cursor-aware reconnect #184, live macro
progress #188. Two clean review lenses (security-auditor, code-simplifier);
govulncheck 0 CVEs. When the loop resumes, the queued candidates are: GUI true
per-event delta apply (it still full-refreshes per lifecycle frame — the reviews
judged that model clean, so lower priority), or a NEW area (provider coverage,
observability, DX). No open work blocks a resume.

## Notes

- Model selection for subagents: haiku=mechanical, sonnet=standard, opus/fable=hard reasoning; reserve opus for <10%.
- Per-commit self-review trio (code/security/simplify) then fold findings forward; never amend a reviewed commit.
- CodeRabbit/bot rate-limit red check = false-flag; the signal is the review threads (resolve via GraphQL), not the check status.
