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

- [x] QUEUE DRAINED. Every PR from this arc landed: #290 #292 #293 #294 #295
      #297 #298 #299 #300 #302. Zero open non-release PRs, zero unresolved
      threads, zero failing checks.
      (#299 landed: config write_paths is verified from an operator's toml.)
      The containment stack all LANDED: #292 #294 #295 #297 #298.
      #290 and #293 LANDED. Hash-prefixed deliberately: guard 9 extracts
      `#[0-9]{3}` tokens from THIS line, so bare numbers would be invisible to
      it and a stale list would pass the check.
      (225, 274, 272, 275, 277 and 278 landed this pass.)
      All four are QUEUED or auto-merge ARMED with zero unresolved threads and
      zero failing checks; the only remaining work is CI on a serialized runner
      pool. Pushing more at that pool measurably slows it (verified earlier this
      session). Re-check with scripts/verify-repo-claims.sh before asserting.
      (225, 274, 272 and 275 already landed this pass.)
      All review threads resolved; each fix carries its own negative proof.
        * #272 differentFrom enforcement -- MERGED. FIVE review findings, all real:
          native fan-out bypassed the check entirely (coalesced groups return
          before the per-step loop, so the reviewer WAS the author), rotation
          overrode `providers`, a padded peer ID passed import then never
          resolved, `providers` was unenforced on the fan-out path too, and a
          differentFrom CYCLE deadlocked forever with no operator-visible
          state. The fan-out rule is now stated ONCE -- a group is one binding
          chosen before any member is examined, so NO per-step restriction can
          be honoured by a coalesced turn -- because the field-by-field framing
          is exactly what let the `providers` twin slip.
        * #275 PTY cleanup budget (#273 family) -- MERGED. Product bug: the budget was
          attempts*interval, but each pass walks the whole process table, so
          under load 100 attempts elapse well before 500ms and abort a
          CONVERGING cleanup. Now a 2s wall-clock deadline. The debugger's
          third fix was DROPPED -- it could not be independently proven.
        * #277 live contained turn -- MERGED. Its TestMain also answers the
          Linux containment-helper re-exec, without which the contained turn
          could never start and would have read as a PRODUCT failure.
        * #278 claude API-failure categorization -- MERGED. Its first version
          fixed the diagnosis and BROKE the retry policy: mapping an unstatused
          api_error to ErrClaudeServiceUnavailable made a terminal auth failure
          retryable, when the generic sentinel was already correctly terminal.
          Naming a category is choosing a retry policy.

- [x] CONTAINMENT ARC COMPLETE. All three shipped providers now run a real
      turn CONFINED -- claude, codex, opencode, every one status=done. It
      started from codex being unable to run under containment at all.
      What shipped: #290 a declared per-binding containment capability, with
      dispatch REFUSING an unhonourable request instead of silently downgrading
      it; #292 the default flipped ON; #297 narrow per-provider write
      allowances; #298 the measured paths (~/.codex, ~/.local/share/opencode)
      declared and wired through to the kernel policy; #293/#294 the docs.
      THREE wrong turns of mine are worth keeping, since each has a general
      shape:
        * I declared "no policy widening exists" after ONE bisection step. A
          negative needs more evidence than a positive.
        * Codex had TWO distinct failures (app-server startup, then the result
          file) and I fixed only the second, saw no change, and concluded the
          path was irrelevant. Each fix alone leaves the other standing.
        * I shipped it darwin-only; Linux CI caught that ExtraWritable never
          reached Landlock. A platform-specific implementation needs a
          platform-specific check.

- [x] Issue #273 CLOSED, fix landed in #288. NOT a product bug: the ceiling
      trips in ~370ms and the full runner path returns the right error in 2.06s
      -- but 39.73s under -race, a ~19x multiplier on a 1 MiB/line firehose.
      The 90s test budget left ~50s of headroom, which CPU contention consumed.
      Budget is now build-tagged so the instrumented case gets room without
      loosening the ordinary build, where the same path costs 2s.
      THE LESSON: "the ceiling did not trip" and "the ceiling tripped too slowly
      to observe" produce the IDENTICAL error. Only timing the two paths apart
      distinguishes them, and I filed #273 on the first reading without
      measuring -- the same shape as blaming the harness in #276.
      Original report: it is LOAD-dependent,
      not a code regression. Passes in ~40s on an idle machine at every commit I
      tried (including before #275). Under ~12 spinners/core it fails at exactly
      90.01s -- the TEST's own context deadline -- with "context deadline
      exceeded, want ErrObservedOutputTooLarge". So the open question is whether
      the ceiling is genuinely too slow to trip (a PRODUCT issue: an endless
      provider runs 90s before being stopped) or the test is under-budgeted.
      Establish that before changing either. Original report:
      the observed-output ceiling does not trip under -race on
      main. TestOpencodeRunnerBoundsEndlessNonJSONProgress and
      TestCodexRunnerBoundsEndlessObservationalProgress both hit the 90s
      deadline instead of ErrObservedOutputTooLarge. Reproduced on clean main
      at 1b832a9, so it is not from any open branch. Establish first whether
      the ceiling is unenforced on that path or the test never reaches it --
      product bug vs inert test. Same family as the PTY reaping item below.

- [x] CLOSED: PTY process-group reaping. FIXED by #275's wall-clock cleanup
      budget, and the item's own hypothesis was right -- cleanup WAS silently
      giving up under load, because the budget was attempts*interval while each
      attempt also paid for a full process-table walk. Under contention the
      attempts were consumed long before the intended wall-clock elapsed.
      VERIFIED under the load that originally produced it (~12 spinners/core,
      the same shape that reproduced the failure): both
      TestAgentObservedOutputCeilingKillsReapsDiscardedEndlessLine and
      TestGroupSignalConvergesBeforeReturning pass at -count=3. An idle pass
      would have proven nothing here -- the failure was never reproducible
      idle, which is what made it look CI-only for so long.
      Original report follows.
      PTY process-group reaping: cleanup can leave a live child.
      Surfaced in a merge_group run of #252 (a PR touching only two workflow
      YAML files, so the bug is on MAIN):
        TestAgentObservedOutputCeilingKillsReapsDiscardedEndlessLine
        agent: PTY process group 2709 still has live members after cleanup:
        [2710], want static ErrObservedOutputTooLarge
      After the agent kills the group for exceeding the output ceiling, a child
      survives cleanup, so the error wraps and no longer matches the sentinel.
      POTENTIALLY A REAL PRODUCT BUG, not just a test-timing issue: if the kill
      path does not actually wait for the group to die, a real provider turn can
      LEAK PROCESSES. That is the never-block invariant's blind spot -- cleanup
      must not hang the supervisor, but it also must not silently give up.
      NOT reproducible locally (4+ runs pass on an idle machine); same CI-only,
      load-sensitive profile as the watchdog flake fixed in #267.
      Dispatched to stuck-loop-debugger WITH the ruled-out list, and the
      explicit question of test-bug vs product-bug -- that determines the fix
      and must not be guessed. Escalated immediately rather than after four
      wrong theories, which is what the watchdog flake cost.
      WAIT-labelled 2026-07-28 after verifying NOTHING is actionable: zero DIRTY,
      zero unresolved threads, and #252's only "failure" is STALE -- Package GUI
      reads FAILURE on its PR branch but completed/SUCCESS on its queue branch
      2f9f2549, which carries #270's appimagetool fix. A queued PR also CANNOT
      be rebased, so there is no way to refresh the PR branch even if I wanted
      to.
      #257 and #252 are in the queue (19/23 checks done on #257's branch);
      #225 and #251 are running checks after their rebases, down from 22
      outstanding to 9.
      RE-LABEL TO `[ ]` the moment any PR goes DIRTY, gains an unresolved
      thread, or shows a failure that is ALSO failing on its queue branch --
      verify-repo-claims.sh guard 8 fails the verifier if this label outlives
      its justification.
      MERGED via the queue this session: #271, #251, #252, #257, #262, #222, #270,
      #255,
      #268,
      #267, #263,
      #259, #265, #261, #258, #236, #247, #256, #245, #246.

      MECHANISM: GitHub merge queue (ruleset 19896999, squash, ALLGREEN, batches
      up to 5). The driver is STOPPED and obsolete. Three user-directed changes
      made this work:
        1. MERGE QUEUE -- the structural fix for `strict` protection. The queue
           tests base + PR + everything ahead of it ONCE, in order, instead of
           making every author rebase into a moving target.
        2. REQUIRED CHECKS 25 -> 11. CD re-validates every build/package target
           via GoReleaser, so CI gating them re-ran at merge time what CD runs
           at release time. CodeQL came out too: codeql.yml excludes
           gh-readonly-queue/** and is fleet-synced from jbdevprimary/gh-fleet-sync
           ("Do not edit in place"), so its two required checks could NEVER
           report on a queue branch. Everything dropped still RUNS on each PR.
        3. ci.yml is PR + merge_group ONLY. The push trigger re-ran the whole
           matrix on a tree the queue had just tested.

      QUEUE MECHANICS worth knowing:
      * `gh pr merge --auto` DOES enqueue; "The merge strategy for main is set
        by the merge queue" is INFORMATIONAL, not an error. `--delete-branch` is
        rejected -- the queue owns deletion.
      * Query entries with entries(first:N){totalCount ...} and trust
        totalCount; the nodes list can come back empty while entries exist.
      * A queue entry reading UNMERGEABLE while the PR reads CLEAN means it
        conflicts with the BATCH ahead of it -- the queue doing its job.
      * ALLGREEN dissolves the whole batch when one entry fails, ejecting
        PASSING entries too. They are re-added automatically. Measured 1
        dissolution against 4 successes -- not worth reconfiguring.
      * Each queue branch carries ~23 check-runs. A snapshot showing required
        checks "missing" usually means NOT YET COMPLETE. Watch completed-vs-
        running across two samples before calling it a stall.
      * A FAILING check on the PR BRANCH can be STALE once the PR is queued.
        The queue tests base+PR, so a fix that landed on main after the PR's
        last push IS present in the queue branch even though the PR branch
        lacks it. #252 showed Package GUI failing while its queue branch
        2f9f2549 had #270's appimagetool fix and was re-running that job green.
        Check the gh-readonly-queue/* branch before acting on a PR-branch
        failure -- and note a queued PR CANNOT be rebased ("Branches that are
        queued for merging cannot be updated"), so dequeuing to "fix" a stale
        failure would be strictly worse than leaving it.

      ACTIONS, in order:
      1. `bash scripts/verify-repo-claims.sh` -- never assert status from memory.
      2. DIRTY -> resolve. Most conflicts are GENERATED docs
         (docs/api/internal/*.md): `make docs-api`, then `git add` the path.
         docs/design/index.md conflicts are additive -- keep BOTH entries.
      3. FAILING -> read the job log. Re-check before believing it: CI re-runs
         constantly and a FAILURE seen once is often already gone.
      4. Unresolved thread -> verify against the code, then fix or counter it.
      5. Nothing actionable -> the queue is working; do not add CI load.

- [x] docs/design/exact-provider-identity.md — DONE (PR #255). Documents
      Invocation/StrictBinding/InvocationConfigHash and the transferable rule:
      compare against the RESOLVED value, not the shape of the thing that
      produced it. Four tests execute the doc's claims (the real substitution,
      the strict refusal, every fingerprint property incl. stability, and the
      limits section), so the page cannot drift from the code.

- [x] `differentFrom` REFERENCES validated at import (PR #259). An unresolvable
      reference is silently VACUOUS — the plan reads as carrying an independence
      guarantee while nothing enforces it, which is worse than no field — and a
      self-reference is unsatisfiable, so dispatch could only block it forever.
      walkPlanSteps derives ids the way import does, so validation and dispatch
      share one notion of identity. Guide says validated-not-enforced.
      - [x] CLOSED (#272, bf77756; verified on origin/main -- resolveIndependentBinding
            and validateDifferentFromAcyclic both present): populate the
            independence domains, THEN enforce differentFrom.
            UNGATED 2026-07-28: #262 and #263 are both MERGED, and re-verified
            on origin/main rather than trusted -- recordExecutionProvenance
            present in internal/orch/orchestrator.go, HandleCalibrationPut
            present in internal/supervisor/calibration.go. Step 3 (enforcement)
            is BUILT and in review as #272, including the three findings that
            review surfaced: native fan-out bypassed the check entirely,
            rotation overrode `providers`, and a padded peer ID never resolved.
            Closes when #272 lands.
            Historical note on why this WAS gated: building enforcement before
            the domains had a writer would have compared "" against "" and
            permitted everything -- the same vacuous guarantee, one layer in.
            Original scoping follows. #236
            (9c04550) shipped the STORAGE for both halves, and I briefly marked
            this ungated on that alone. Checking the write paths corrected it:
              * store.Calibration.IndependenceDomain exists
                (internal/store/calibrations.go:58), but NO production code
                records a calibration — only tests do.
              * store.TaskMetadata.AssignedIndependenceDomain exists
                (task_metadata.go:35) and RecordTaskExecution writes it
                (task_metadata.go:224), but that function has ZERO production
                callers — again only tests.
            So both columns are empty in every real run, and enforcement built
            on them today would compare "" against "" and permit everything —
            the exact VACUOUS GUARANTEE #259 rejects at import, reintroduced at
            dispatch and harder to see. Order is therefore:
              1. [x] DONE — PR #262. Dispatch now calls
                 RecordTaskExecution before every turn, in BOTH the single-task
                 and native-fan-out paths. The domain comes from the binding's
                 CALIBRATION (GetCalibrationByAlias), not derived from the
                 provider name -- inferring claude->"anthropic" would put a
                 vendor table in the dispatch path and disagree with whatever a
                 calibration later measures. Uncalibrated binding => empty
                 domain, turn still runs (a missing calibration is not an error).
                 Model/effort come from provider.ResolveInvocation, NOT the
                 request: review caught that neither dispatch path sets
                 req.Model/req.Effort, so recording them wrote EMPTY values while
                 looking like it captured them (40e73db). Same commit gates the
                 domain on InvocationHash equality -- alias match alone reused a
                 calibration measured for a different command line, and a stale
                 domain is worse than none because it looks measured. Best-effort with an emitted
                 event on failure: provenance records a turn, it does not gate
                 one. Fan-out records EVERY task in the group -- proven by
                 negative test: recording only claimed[0] leaves peer "0.1" with
                 an empty domain, which reads as INDEPENDENT and would satisfy
                 differentFrom for the tasks that most obviously violate it.
              2. [x] DONE -- PR #263, but NOT by porting the v2 lane. Scoping it
                 showed the v2 handler depends on APIs main does not have
                 (PutProviderCalibration, ValidateProviderCalibration, an ipc
                 calibration surface) while main has RecordCalibration instead,
                 so a verbatim port would BE the "never port the diff" mistake
                 the spec warns about. Built the producer against MAIN's API
                 instead: calibration-put/calibration-list on the drive version,
                 a separate optional CalibrationHandler so no existing handler
                 breaks, no client-supplied id (the store content-addresses),
                 and a conflicting re-record REFUSED with CodeConflict rather
                 than overwritten -- silently replacing an alias's calibration
                 would retroactively change what every already-dispatched task
                 is believed to have run on. The full admission lane
                 (calibration_admission.go, await-calibration tasks,
                 repetitions) remains unported and is a separate increment.
                 Original scoping note: nothing in the tree records one
                 (grep for RecordCalibration outside store/ + its tests returns
                 nothing), so #262's domain lookup finds none and correctly
                 records empty. The lane is already DESIGNED, not to be
                 improvised: docs/superpowers/specs/2026-07-26-dag-integration-design.md
                 lines 118-144 specify internal/orch/calibration_admission.go
                 (~255 lines + tests) ported as-is, carrying ONE *calibrationLane
                 pointer rather than seven loose dispatchStepArgs fields, and
                 warns NEVER to port orchestrator.go's diff -- hand-apply the
                 readiness swap, since main has since gained native-fanout and
                 stepGateBlocks. store.BindTaskCalibration already exists for
                 await-calibration tasks, so the store side is in place.
                 This is a full increment, not a wire-up.
              3. Only then compare at dispatch and refuse a matching domain.
                 COMPOSITION ALREADY PROVEN (bf53f6a on #262): combining #262 and
                 #263 locally showed they merge with only a generated-doc
                 conflict, build together, and pass end to end --
                 TestOperatorRecordedCalibrationReachesTheTask walks operator
                 record -> dispatch -> domain on the task. This mattered because
                 each half was verified ALONE: the producer writes what an
                 operator supplies, the consumer requires InvocationHash to equal
                 what dispatch resolves, and nothing checked an operator could
                 produce a record satisfying that gate. Had they disagreed, every
                 calibration would read as stale and the domain would stay empty
                 -- the same vacuous guarantee by a longer route. Negative proof:
                 a plausible-but-different hash reds the test.
            Verified by grepping origin/main for callers, not by assuming the
            types being present meant they were wired.

- [x] Increment 11 operator half — DONE (PR #258). Task carries BlockedReason,
      backed by a bulk plan-scoped store.ListTaskBlockingReasons (per-task reads
      would be an N+1 on every operator refresh; ids repeat across plans).
      FOUND WHILE TESTING IT: StateForTask never mapped blocked_capability or
      blocked_input and its default fails the WHOLE projection — one blocked
      task returned "unknown Ralph task status" for the entire snapshot, so the
      status meant to make a stall visible hid everything instead.
      - [x] DONE 2026-07-28 (#304): per-task provenance over the observe
            surface. assigned_alias/provider/model/effort and
            assigned_independence_domain now project store -> observe ->
            TUI (`via=`) + GUI (`via `). LEFT JOIN so a task with no metadata
            row still appears; absent provenance stays empty/omitted rather
            than defaulted, so "never dispatched" cannot read as "ran on the
            pool default". ProvenanceLabel lives on observe.Task because both
            UIs render it and a divergent fallback would make one task read
            differently per surface.
            Each hop negative-proofed SEPARATELY, which is the part worth
            keeping: dropping the observe copy fails the observe test while
            the STORE tests stay green -- proof the second test covers a real
            gap instead of duplicating the first. A single end-to-end test
            could not have distinguished those hops.
      - [x] DONE 2026-07-28 (#304): ready partitions over the observe surface.
            Each task carries PartitionOrdinal through store -> observe ->
            TUI/GUI, so an operator can see that several running tasks are ONE
            fan-out turn rather than that many independent dispatches.
            Two decisions worth keeping:
              * OPAQUE, not the real identity. A partition is (group path,
                declared binding key) and the binding key re-encodes the
                AUTHOR's binding fields, so projecting it would carry
                plan-written text across the boundary that withholds
                descriptions. The ordinal answers "same partition?" and
                deliberately not "pinned to what?".
              * DERIVED THROUGH THE SAME FUNCTION dispatch groups by, never
                recomputed in the snapshot SQL. A second implementation is a
                second DEFINITION of a partition; they diverge the first time
                either changes, and the operator reads a grouping that no
                longer happens. Pinned by TestOperatorTasksAgreeWithReadyPartitions.
            UIs number partitions per view (p1/p2) and label ONLY partitions of
            2+, since a partition of one is the ordinary case and marking every
            row buries the fan-out groups.
            Original gating record for BOTH halves follows -- verified by
            grepping, as this item insists:
              * `func (s *Store) ReadyPartitions` IS on main (#225, refined by
                #282 to split on the declared binding).
              * `recordExecutionProvenance` IS on main (#262), 6 call sites.
            As of 2026-07-28 NEITHER was projected over the observe surface:
            grepping internal/observe/snapshot.go for ReadyPartition,
            AssignedIndependenceDomain, and assigned_alias returned ZERO. So
            this was real work, not a wait -- the gate opened and nobody walked
            through it. BOTH halves are now closed by #304 -- provenance
            (assigned_*) and ready partitions (PartitionOrdinal) each project
            store -> observe -> TUI/GUI.
            Prior gating notes follow. RE-VERIFIED 2026-07-28 after #236 merged
            (9c04550) — the gate was then HALF open:
              * per-task provenance: UNBLOCKED. internal/store/calibrations.go
                is on main, so the types exist to project.
              * ready partitions: still gated. `func (s *Store) ReadyPartitions`
                is NOT on main; it ships with #225, which is open. RE-CHECKED
                after #258 merged (c94ca79): the observe blocked surface IS on
                main now, but that is the operator half, not this one.
            Do the provenance half first rather than waiting on both — checked
            by grepping origin/main for each symbol, not by assuming the pair
            moves together.

- [x] SUPERSEDED by the containment item at the top of Remaining. Kept for the
      evidence trail below, NOT as a second live gate -- two entries for one
      piece of work is how an executor ends up waiting on something already
      done. Provider write containment: reachable from config (DONE), default
      on (#292).
      UNGATED 2026-07-28: #251 is MERGED, and the wiring re-verified on
      origin/main rather than trusted -- vconfig.ContainProviderWrites is read
      by PRODUCTION code at cmd/radioactive_ralph/binding_resolver.go:268, not
      only by tests. That distinction is the whole point here: the key was
      previously parsed and never read, which is the inert-feature defect this
      item exists to close.

      STEP 2 IS STILL GATED. I got this wrong THREE times in one session and
      each correction narrowed it, so the gate is stated once, here, and the
      superseded paragraphs have been DELETED rather than annotated -- an
      executor reading a contradicted claim could flip the default early.

      (1) I first wrote "real-turn evidence is complete" from the CI E2E alone.
          That E2E drives a FAKE CLI, which attempts only what the test scripts,
          so it cannot answer the question the flip turns on: does a REAL CLI
          need something the project-root policy denies?
      (2) I then blamed the harness (#276, isolated HOME stripping credentials).
          Wrong: Environ() applies only to driven SUBPROCESSES, and the live
          tests dispatch in-process with the real environment. #276 is CLOSED as
          invalid.
      (3) The actual cause was a one-word bug -- the stream-json stdin frame
          emitted "Message" instead of "message", so EVERY real claude turn died
          in 0.78s with "Expected message role 'user', got 'undefined'". Fixed
          in #281; a live turn now returns output and usage, and
          TestE2E_LiveDispatchWithRealProviderCLI reaches status=done.

      THE ONE GATE: land #281, then run the #277 live CONTAINED turn
      (RALPH_E2E_LIVE=1, TestE2E_LiveContainedTurnCompletes). If it reaches
      status=done, flip the default. If it fails, the failure IS the
      upgrade-breakage answer this item has been waiting for -- record what the
      policy denied before changing anything.

      EVIDENCE ACQUIRED 2026-07-28 (cdc28d8), and it MOVED the bar for step 2.
      A CodeRabbit finding on #251 was verified true: declarativeStreamJSON
      called runStreamJSONCommand, which took no containment argument at all, so
      a stream-json binding ran UNCONFINED while the config claimed
      "confines the provider process AND everything it spawns". Fail-open on a
      security boundary, and worse than no containment because an operator
      trusts the claim. Enforcement now lives at one choke point
      (applyContainment) and TestEveryDeclarativeShapeConfinesItsTurn measures
      whether a write ESCAPES rather than whether an argument was passed --
      a signature check would not have caught this one. Any new runner shape
      added before the flip needs a subtest there.

      EXEC-PATH AUDIT DONE 2026-07-28 (fc9c4a0) — the precondition step 2 needed.
      Every process-launching site classified:
        CONFINED: provider/exec.go (plain-stdout, last-message-file),
          declarative.go stream-json (fixed cdc28d8), agent/agent.go,
          claudesession/session.go Spawn (wired pre-emptively -- it launches
          `claude -p` with NO containment and no production caller yet, which is
          precisely how stream-json shipped broken).
        DELIBERATELY NOT: orch/verify.go checkCommandExitsZero. It is the
          ORCHESTRATOR running the plan author's acceptance criterion, not a
          provider turn, and those legitimately write outside the project
          (go build -> GOCACHE, test runners -> TMPDIR). The project-root policy
          would fail them and report the task unverified for an unrelated
          reason. If plans ever come from an untrusted source this needs its own
          wider-root policy. Documented in place, not changed silently.
        N/A: doctor, service, projectid, agentdetect, genesis (fixed argv);
          tests/e2e and cassette/recorder (test-only).
      So step 2 is now gated ONLY on #251 merging.
      macOS and Linux both enforce in #251; native Windows is closed as
      not-needed (no provider runs there). Two steps, in order:
      1. [x] DONE on #251, and the WIRE now exists too (998681d). The key was
         INERT when first shipped: vconfig parsed it, exposed
         ContainProviderWrites, and NOTHING called that -- and
         WithProviderWriteContainment had zero production callers. An operator
         setting the key got no containment and no signal it did nothing.
         THIRD instance of this class on this one feature: the field set nowhere
         in production, the stream-json shape that skipped applyContainment, and
         the key nothing consulted. Each looked complete from its own side, which
         is why "is it wired?" is now a standing question, not an afterthought.
         Resolved PER PROJECT (ContainmentResolver function, not a process bool):
         one orchestrator serves every project, so a static flag applies one
         project's answer to all of them. Fails CLOSED-TO-OFF on config error,
         matching the key's contract. Original note: absent
         OR malformed means off — a typo must not silently enable a boundary
         that makes provider writes fail, since that surfaces as unexplained
         provider errors far from the cause. Accepts bool and string because a
         store layer round-trips values as JSON strings.
      2. [WAIT] Flip the default only after real turns have run with it enabled.
         The config key shipped on #251, which is still OPEN, so no real turn has
         used it yet. A provider that legitimately writes to a shared cache
         outside the checkout would start failing on upgrade, and that is not a
         guess worth making from the test suite alone.

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

- [x] #305 MERGED (CLI task table + blocked-reason in all three views + meso
      row width + GUI worker-claim parity).

- [ ] [WAIT] Land the 1 open PR: #306 (partition ordinal gated on
      dispatchability -- the bug DOGFOODING found). Auto-merge ARMED.
      Hash-prefixed deliberately: guard 9 extracts `#[0-9]{3}` from THIS line.

- [x] DOGFOODING DONE 2026-07-28 -- and it paid for itself immediately.
      Authored .radioactive-ralph/plans/observe-surface-followups.md, imported
      it, ran a real supervisor, and read Ralph's own state through the CLI
      table built this session. Two things came out of it:
        * the importer REJECTED my first draft with a precise message
          (narrative paragraph before the list) -- the validator working.
        * `status` showed `build` sharing a partition marker with the three
          tasks declaring after:[build]. Real bug in MY projection: the ordinal
          was computed for every task with no readiness filter, so it
          advertised a grouping dispatch would never perform. Fixed in #306.
      The lesson is the one worth keeping: the agreement test written to catch
      exactly this passed the whole time, because every task in its fixture was
      independently ready and agreement held VACUOUSLY. Running the real system
      on real input found in one command what the test could not.
      (Superseded, kept for the record:) old #305 line follows.
- [x] SUPERSEDED: Land the 1 open PR: #305 (CLI task table + blocked-reason in all
      three views + meso row width). Auto-merge ARMED, both Codex threads
      resolved, no failing checks -- it is working through the merge queue,
      which is remote state I already triggered. A Monitor is watching it.
      Hash-prefixed deliberately: guard 9 extracts `#[0-9]{3}` tokens from
      THIS line.

- [x] #304 MERGED (per-task provenance + ready-partition identity over the
      observe surface). Both halves of the observe item landed with it.

- [x] UNSTACKED 2026-07-28: rebased `feat/cli-task-table` onto the merged main.
      It WAS genuinely stacked -- verified by cherry-picking onto origin/main
      and watching the build fail on PartitionLabels/ProvenanceLabel/
      PartitionOrdinal, all introduced by #304 -- so it could not have opened as
      an independent PR. The rebase dropped 5 commits already squash-merged;
      each skip was confirmed content-on-main FIRST (a past rebase silently ate
      two of my own commits by skipping without checking).

- [x] DONE 2026-07-28: UI/UX pass on the meso views. The suspicion was right --
      every flaw was invisible to a green suite and obvious in the rendered
      output. Both questions it posed had a defect behind them:
        * TUI width: the worst-case row was 215 columns and wrapped
          mid-sentence. Now ~104 (block reason on a continuation line, worker
          id abbreviated to its distinguishing TAIL -- a first pass truncated
          the head and made every row read "worker-…").
        * GUI clipping: Codex caught it before I did. The remediation was an
          HBox cell under a VERTICAL-only scroll, so it clipped at the window
          edge with no way to reach it. Now its own wrapping row.
      Dumping the GUI widget tree then found a THIRD thing neither question
      asked about: the GUI omitted the worker claim entirely while the TUI and
      CLI both showed it, so one task read differently per surface. Fixed;
      all three now render w: / via / pN identically.
      Method note: the first dump walked only Button and Label and reported the
      row as having no status at all -- the chip is a canvas.Text. Reading raw
      output only helps if the reader sees everything the operator does.

- [x] GENERATED 2026-07-28 by forward-exploring the observe surface just
      shipped (#304); both findings resolved in that PR. Verified against the code before writing down, not
      inferred:
      * The full consumer chain WAS checked and is sound: IPC uses type
        ALIASES (`type ObserveSnapshotReply = observe.Snapshot`,
        internal/ipc/protocol.go:315), so no field can be dropped there, and
        `query --json` serializes the whole snapshot. No work needed -- recorded
        so the next pass does not re-derive it.
      * REAL GAP: the human-readable `query` output is a single summary line
        (project/plans/tasks/active_workers/captured_at, query_cmd.go:146). It
        never lists tasks at all, so an operator without `--json` cannot see
        status, provenance, or partitions -- the TUI and GUI are the only way to
        read what the snapshot now carries. Decide whether the CLI should grow a
        task table; if yes it must honour the same rules the UIs do (omit absent
        provenance, label only partitions of 2+, never print the raw ordinal).
      DECIDED + DONE 2026-07-28 (same PR): yes, the CLI grows a task table.
      `status` now prints one line per task under the summary, reusing
      observe.PartitionLabels and Task.ProvenanceLabel rather than
      re-implementing the rules -- a CLI with its own copy would be a third
      dialect drifting from the TUI and GUI. A truncated page says so, since a
      bounded list that looks complete reads as "the rest finished".
      LESSON, and the reason to always read the actual output: the tests passed
      while every unrun task carried TRAILING WHITESPACE, because the status
      column is padded for marker alignment and unrun tasks have no markers.
      No assertion covered it -- reading the real bytes did. Trimmed, and pinned
      by TestRunStatusQueryTaskLinesHaveNoTrailingSpace.

- [x] DONE 2026-07-28 (same PR): folded the duplicated partition-labelling loop
      into observe.PartitionLabels. I had written the SAME display policy twice
      (internal/tui/meso.go and internal/gui/views.go) in this session -- the
      2+ threshold and the never-show-the-hash rule -- which is the drift
      ProvenanceLabel was already centralized to avoid. Fixed on the spot rather
      than queued, since a duplication is cheapest to remove before a third
      copy exists. Numbering is first-seen order, pinned by a repeat-run test so
      map iteration cannot reshuffle labels between identical renders.

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
