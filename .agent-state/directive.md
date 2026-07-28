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

## Shipped

Compressed to docs/superpowers/PILLARS.md and the git log. This file tracks
only what is LEFT. Merged in the current arc: #212, #215, #216, #217, #219,
#220, #221, #224, #226, #228, #229, #230, #231, #232, #233, #234, #235, #237,
#238, #240, #250 — releases through v0.30.0.

## Remaining

- [ ] Land the 11 open PRs: 222, 225, 236, 245, 246, 247, 251, 252, 255, 256, 257.
      There is ALWAYS an action here; this is not a wait.
      1. `bash scripts/verify-repo-claims.sh` first — never assert status from
         memory.
      2. DIRTY -> resolve. A semantic conflict needs the correct half CHOSEN;
         `--ours`/`--theirs` by reflex is how a fix gets reverted (main's
         held-client config source vs #245's per-call dial was exactly this).
      3. FAILING -> read the job log, fix the cause.
      4. BEHIND -> `gh pr update-branch`.
      5. Unresolved thread -> verify against the code, then fix or counter it
         with evidence.
      6. Nothing to do on any PR -> work an item below. Do not report status.

- [x] docs/design/exact-provider-identity.md — DONE (PR #255). Documents
      Invocation/StrictBinding/InvocationConfigHash and the transferable rule:
      compare against the RESOLVED value, not the shape of the thing that
      produced it. Four tests execute the doc's claims (the real substitution,
      the strict refusal, every fingerprint property incl. stability, and the
      limits section), so the page cannot drift from the code.

- [ ] Enforce `differentFrom`. It is the last ralph-task field with no reader
      (`requires` #247, `providers` #250, `inputs`/`outputs` #228/#229 all
      landed). It names tasks that must not share this task's INDEPENDENCE
      DOMAIN. #234 shipped the invocation identity; #236 (calibrations, with
      inference/control/independence domains) is still open, so build the
      comparison against the domain #236 defines and land after it.

- [x] Increment 11 operator half — DONE (PR #258). Task carries BlockedReason,
      backed by a bulk plan-scoped store.ListTaskBlockingReasons (per-task reads
      would be an N+1 on every operator refresh; ids repeat across plans).
      FOUND WHILE TESTING IT: StateForTask never mapped blocked_capability or
      blocked_input and its default fails the WHOLE projection — one blocked
      task returned "unknown Ralph task status" for the entire snapshot, so the
      status meant to make a stall visible hid everything instead.
      - [ ] Remaining half: ready partitions + per-task provenance over the
            observe surface. Gated on #225 (ReadyPartitions) and #236
            (calibration provenance), neither on main yet.

- [ ] Provider write containment beyond macOS+Linux. Both ship in #251
      (sandbox-exec; Landlock via a re-exec helper). Native Windows is CLOSED
      as not-needed — no provider runs there (creack/pty returns
      ErrPTYUnsupported). What remains is turning containment ON by default
      once enough real turns have run with `WithProviderWriteContainment(true)`
      to know nothing legitimate writes outside a checkout.

- [x] #248 CLOSED (PR #256). VerifyAndCompleteAs takes the REPORTING session so
      the store's owner guard compares against the worker that produced the
      evidence; a stale reporter now loses benignly instead of overwriting its
      replacement. Both dispatch paths wired, with a source-level test asserting
      that wiring — a correct guard nothing calls is the same defect shape as
      containment shipping with zero callers.

- [x] #249 CLOSED (PR #257). dispatch.saturated names how many ready candidates
      went UNEXAMINED, so the event says work is queued behind a full pipeline
      rather than merely that a slot was taken. Transition-only per #247;
      releaseDispatchSlot re-arms it. Both exits covered (per-step loop and
      fan-out group), -race clean since the flag is touched concurrently.


## Rolling improvement queue (directive 0 appends here)

- [x] LEFTOVERS AUDIT (2026-07-28 UTC) — DONE, this was the investigation itself. Prompted by the right question — I had
      verified the merged branches were ABSORBED, which is not the same as
      their WORK being finished. Audited main for unfinished scope and found
      three genuine items, each verified against the code rather than inferred:

- [x] Provider write-side containment — SHIPPED for macOS (PR #251).
      internal/contain wraps the provider in sandbox-exec (Seatbelt) with an
      absolute, SYMLINK-RESOLVED root; opt-in per agent.Start via
      ContainmentRoot (NOT derived from Dir — where a process starts is not the
      same claim as the only place it may write); fails closed on an
      unsupported platform or a relative root.
      Inheritance is the load-bearing property: Seatbelt applies the policy to
      the process AND everything it spawns, so a fan-out provider's sub-agents
      cannot escape. Grandchild test included.
      LESSON — a convenience grant silently re-opened the boundary. The draft
      profile granted the resolved TMPDIR for scratch files; on macOS that
      resolves under /private/tmp, so it allowed writes to a subtree full of
      other tools' files WHILE STILL REPORTING CONTAINMENT. Only the behavioral
      test caught it (the escape target wrote successfully). A grant that
      widens the boundary is worse than no containment because it reports
      success. No temp grant now, plus a regression test asserting every
      writable subpath resolves to the root — it inspects the PROFILE's grants
      rather than scanning argv, since in tests the root itself lives under a
      temp dir and a substring scan cannot tell intended from extra.
      The profile allows by default and denies WRITES only: default-deny would
      have to enumerate every read/mach-lookup/network call each CLI version
      needs, and its first omission looks like a provider bug rather than a
      policy one.
      - [x] Linux containment — SHIPPED (PR #251). Landlock via a RE-EXEC
            helper: Wrap re-invokes Ralph's own binary with a sentinel flag,
            that helper restricts itself and immediately execs the provider.
            main() handles the sentinel FIRST — work before the exec is either
            outside the restriction or discarded.
            WHY re-exec and not in-process: Landlock is applied by a process to
            ITSELF, and below ABI 8 there is no TSYNC, so restrict_self binds
            only the CALLING thread. MEASURED on ABI 6: a thread created BEFORE
            the call writes outside the root successfully. Go's runtime has
            threads running before any user code, so in-process would ship a
            boundary with a hole while reporting containment.
            LESSON — the handled-rights mask cost the most time. Landlock denies
            a HANDLED right everywhere no rule grants it. A mask written as "all
            write bits" (0x7fe) actually handles bit 2 READ_FILE and bit 3
            READ_DIR; granting them only under the root made every file outside
            unreadable — including the provider binary and the dynamic loader —
            so execve failed EACCES, a symptom pointing at exec rather than
            reads. Root-caused by stuck-loop-debugger after I burned several
            cycles on it. Three tests now guard the mask directly.
            SECOND LESSON — the behavioral tests build a REAL helper binary.
            Re-executing the go test binary makes it reject the sentinel and never
            run the command, which every "did the file appear?" assertion reads
            as successful containment. That false pass was OBSERVED before being
            fixed; it is the same trap the macOS TMPDIR grant set.
      - [x] Windows containment — NOT NEEDED, closed as a DECISION rather
            than left as a gap (PR #251). No provider can run on native
            Windows: agent.Start allocates a pty via creack/pty, which returns
            ErrPTYUnsupported, and the Windows SCM safety spec says it
            outright — "Native Windows provider workers are already
            unsupported". Windows operators run under WSL, which is Linux, so
            Linux containment is what covers them. Building it would guard a
            code path that cannot execute, i.e. dead code, which this repo
            treats as a defect. A test records the reasoning and FAILS if
            Available() ever reports true on Windows — meaning a provider path
            was added and the matrix needs revisiting; it asserts the CONTRACT
            rather than the platform so it stays meaningful if that happens.

- [x] `desktop_launch.go` — DONE (PR #246). Was the LAST CLI store reader.
      It resolves the project for a Finder/Explorer launch before any supervisor
      is known to be running, which is why it was exempted rather than fixed
      with init: it needs NON-MUTATING resolution, because a file-manager launch
      has an arbitrary cwd (often not a repo, sometimes "/") and project-ensure
      creates on a miss — which would register that directory as a durable
      project just because someone double-clicked an icon. Fixed by adding
      ProjectEnsureArgs.ResolveOnly, which skips the accumulate/touch writes on
      the FOUND path too, not only the create on the miss path: a "resolve" that
      still mutates rows is not one. The gate proved it again — removing the
      import made the boundary test demand the exemption be deleted. Only
      supervisor_cmd.go and binding_resolver.go remain exempt, both
      supervisor-side by design, so a new entry now means a NEW violation.

- [x] Two LOAD-SENSITIVE timing tests — FIXED (PR #237). Both verified not
      caused by the branch that surfaced them (each passed 3/3 and 4/4 in
      isolation on that same branch):
      `internal/provider/claudesession.TestResumeSendsSentinelOnSpawn` and
      `internal/provider.TestDeclarativeProgressRenewsStallButTotalDeadlineStillWins`.
      Both assert which of two racing deadlines fires first, so under
      full-suite parallel load the host's scheduling decides the outcome
      instead of the code. Two independent tests failing the same way is a
      PATTERN, not two flakes. Fix on their own terms — inject a clock, or
      widen the margin between the two deadlines so scheduling cannot invert
      them. Fixed by widening the margins, NOT by faking a clock: what these
      tests verify is that one REAL deadline beats another REAL deadline, and a
      faked clock would verify the comparison while removing the wiring under
      test. Declarative: 400ms stall lease -> 3s against a 40ms emit interval
      (still far below the 1200ms turn deadline it proves wins). Sentinel: the
      5s spawn ctx also bounded the FAKE CLI's life, so a slow machine killed it
      before it echoed and surfaced as "events closed without echo" — a message
      that reads like a missing sentinel rather than an expired budget, which is
      what made it misleading; now 60s ctx with a 30s echo wait below it, so a
      real failure reports "timeout waiting for sentinel echo". Both have
      negative proofs: raising the turn timeout to 8s fails at 8.002s, and
      suppressing the fake echo fails with the specific timeout message.

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
