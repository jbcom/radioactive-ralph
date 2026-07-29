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
        - #272 differentFrom enforcement -- MERGED. FIVE review findings, all real:
          native fan-out bypassed the check entirely (coalesced groups return
          before the per-step loop, so the reviewer WAS the author), rotation
          overrode `providers`, a padded peer ID passed import then never
          resolved, `providers` was unenforced on the fan-out path too, and a
          differentFrom CYCLE deadlocked forever with no operator-visible
          state. The fan-out rule is now stated ONCE -- a group is one binding
          chosen before any member is examined, so NO per-step restriction can
          be honoured by a coalesced turn -- because the field-by-field framing
          is exactly what let the `providers` twin slip.
        - #275 PTY cleanup budget (#273 family) -- MERGED. Product bug: the budget was
          attempts*interval, but each pass walks the whole process table, so
          under load 100 attempts elapse well before 500ms and abort a
          CONVERGING cleanup. Now a 2s wall-clock deadline. The debugger's
          third fix was DROPPED -- it could not be independently proven.
        - #277 live contained turn -- MERGED. Its TestMain also answers the
          Linux containment-helper re-exec, without which the contained turn
          could never start and would have read as a PRODUCT failure.
        - #278 claude API-failure categorization -- MERGED. Its first version
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
        - I declared "no policy widening exists" after ONE bisection step. A
          negative needs more evidence than a positive.
        - Codex had TWO distinct failures (app-server startup, then the result
          file) and I fixed only the second, saw no change, and concluded the
          path was irrelevant. Each fix alone leaves the other standing.
        - I shipped it darwin-only; Linux CI caught that ExtraWritable never
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
      - `gh pr merge --auto` DOES enqueue; "The merge strategy for main is set
        by the merge queue" is INFORMATIONAL, not an error. `--delete-branch` is
        rejected -- the queue owns deletion.
      - Query entries with entries(first:N){totalCount ...} and trust
        totalCount; the nodes list can come back empty while entries exist.
      - A queue entry reading UNMERGEABLE while the PR reads CLEAN means it
        conflicts with the BATCH ahead of it -- the queue doing its job.
      - ALLGREEN dissolves the whole batch when one entry fails, ejecting
        PASSING entries too. They are re-added automatically. Measured 1
        dissolution against 4 successes -- not worth reconfiguring.
      - Each queue branch carries ~23 check-runs. A snapshot showing required
        checks "missing" usually means NOT YET COMPLETE. Watch completed-vs-
        running across two samples before calling it a stall.
      - A FAILING check on the PR BRANCH can be STALE once the PR is queued.
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
              - store.Calibration.IndependenceDomain exists
                (internal/store/calibrations.go:58), but NO production code
                records a calibration — only tests do.
              - store.TaskMetadata.AssignedIndependenceDomain exists
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
              - OPAQUE, not the real identity. A partition is (group path,
                declared binding key) and the binding key re-encodes the
                AUTHOR's binding fields, so projecting it would carry
                plan-written text across the boundary that withholds
                descriptions. The ordinal answers "same partition?" and
                deliberately not "pinned to what?".
              - DERIVED THROUGH THE SAME FUNCTION dispatch groups by, never
                recomputed in the snapshot SQL. A second implementation is a
                second DEFINITION of a partition; they diverge the first time
                either changes, and the operator reads a grouping that no
                longer happens. Pinned by TestOperatorTasksAgreeWithReadyPartitions.
            UIs number partitions per view (p1/p2) and label ONLY partitions of
            2+, since a partition of one is the ordinary case and marking every
            row buries the fan-out groups.
            Original gating record for BOTH halves follows -- verified by
            grepping, as this item insists:
              - `func (s *Store) ReadyPartitions` IS on main (#225, refined by
                #282 to split on the declared binding).
              - `recordExecutionProvenance` IS on main (#262), 6 call sites.
            As of 2026-07-28 NEITHER was projected over the observe surface:
            grepping internal/observe/snapshot.go for ReadyPartition,
            AssignedIndependenceDomain, and assigned_alias returned ZERO. So
            this was real work, not a wait -- the gate opened and nobody walked
            through it. BOTH halves are now closed by #304 -- provenance
            (assigned_*) and ready partitions (PartitionOrdinal) each project
            store -> observe -> TUI/GUI.
            Prior gating notes follow. RE-VERIFIED 2026-07-28 after #236 merged
            (9c04550) — the gate was then HALF open:
              - per-task provenance: UNBLOCKED. internal/store/calibrations.go
                is on main, so the types exist to project.
              - ready partitions: still gated. `func (s *Store) ReadyPartitions`
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

- [x] OBSERVED 2026-07-29, no action needed -- the control invariant caught in
      the wild. A self-test step (`lint-internal`) failed with
      `failure_category: interactive_prompt`: the provider asked for
      interactive input, the watchdog classified it (BlockReasonPrompt,
      watchdog.go:26) and killed the turn, and the TASK failed rather than the
      system blocking. That is the never-block guarantee working, observed on a
      live run rather than in a test.
      Recorded because the first read of a `failed` task is usually "product
      bug". Here the failure IS the product working: a blocked turn that
      surfaces as a failed task with a named category is the designed outcome.
      The operator surface carried it end to end -- category on the row, no
      raw provider text, and the dependent steps naming their root blocker.

      ROOT CAUSE FOUND LATER, and "no action needed" was premature -- the
      HANDLING was correct, but the trigger was fixable. A STALE LINTER CACHE
      invents work: `golangci-lint run ./internal/...` reported 11 issues, all
      11 in `../.worktrees/rr-sandbox/` -- not a worktree of this repo, absent
      from any go.work, files not on disk. After `golangci-lint cache clean`
      the same command reports `0 issues`.
      So the agent was asked to fix findings it COULD NOT fix (the files are
      gone), tried, and asked for guidance. That is a diligent agent meeting an
      impossible task, not a confused one.
      This also explains the OTHER anomaly in the same run: the four-file
      integer-overflow guard was written to satisfy PHANTOM lint errors. Both
      anomalies, one cause.
      The general tell: a lint failure naming a path outside the repo. Check
      the file exists before believing the finding. Guide updated.

- [x] #321 MERGED. Folded three overlapping AGENTS.md rules into one organized
      by layer -- check, setup, threshold. All PRs landed: open=0, queued=0.


- [x] DOGFOOD RESULT 2026-07-29, self-test on merged main. TERMINAL: 9 done,
      1 failed (`lint-internal`), 2 blocked (`claims`, `e2e`) = 12. The plan
      reports DEAD (no runnable work), not COMPLETE, which is the honest
      outcome when a step fails.
      `interactive_prompt` is lint-internal's failure_category -- the REASON
      for that one failure, not a second failed step. Read as two failures the
      arithmetic lands at 11/12 and the record contradicts itself; an earlier
      revision of this entry did exactly that by freezing a mid-flight 8/12
      while `race` was still running.
      The run reproduced its own documented hazards, which is the point of
      running it:

      1. TRANSITIVE BLOCKER, proven live. `claims` declares `after: [e2e]` and
         nothing else, yet reported `blocked_by=lint-internal` -- the root
         failure two hops away, not the intermediate one. That is the #309
         recursive CTE doing its job on a real plan.

      2. THE GUARD-FOR-A-NONEXISTENT-LINT, reproduced exactly. A provider turn
         wrote a plausible, compiling integer-overflow guard across the same
         four `*_unix.go` files the self-test guide predicts. Verified by
         stashing the edits and re-running golangci-lint: ZERO G115 findings.
         The only issues reported were in a stale `.worktrees/rr-sandbox` path
         whose files no longer exist. Edits discarded.
         The doc claimed this happens; the run confirmed it rather than merely
         restating it.

      3. TWO WORKER EDITS TO TESTS THAT WERE CORRECT, and were kept. The
         reflex is to distrust an agent rewriting tests, since that can delete
         coverage -- but checking beat assuming, in the opposite direction from
         (2):
         - supervisor_test.go: the original cancelled the context BEFORE
           collecting results, so a scheduler-delayed starter could acquire
           the lock after the winner released it and then fail CreateSession
           on the shared cancelled context -- a sequential restart miscounted
           as an error. The rewrite waits for the loser's refusal first.
           Ordering assumption checked, not assumed: 40/40 under -race.
           waitForSupervisor returning means the winner is SERVING, so the
           loser has already lost Acquire, and the winner cannot return first
           because it does not return until cancel().
         - discovery_posix_test.go: creates the directory that actually holds
           the socket instead of assuming it equals runtimeDir, and uses
           live.SocketPath rather than re-deriving a path that may not match.

      The standing lesson, now with a counterexample on each side: unauthored
      edits need VERIFICATION, not a policy. Blanket-reverting would have
      discarded two real fixes; blanket-keeping would have shipped dead code
      for a lint error that does not occur.

      4. A REAL PRODUCT FINDING, which is what dogfooding is FOR. The `race`
         step sat at reclaim_count=2 and I first misread it as a stuck row --
         because I filtered on `assigned_worker_id` when the live field is
         `claimed_by_worker_id`. It was not stuck; it was being reclaimed.
         Root cause: two independent bounds govern a turn and the SHORTER one
         bites. The turn deadline is 30m (enormous headroom), but the progress
         lease is 3m and renews on OUTPUT. `go test -race ./internal/store/`
         prints ONE `ok` line after 138.6s of silence -- 41s of headroom --
         so under a concurrent run the watchdog read a working step as a hung
         provider and killed it. Twice.
         First fix was `-v`, verified to stream with sub-second gaps -- and a
         reviewer (codex, PR 322) showed that is NOT sufficient for the path
         that actually failed. Under Codex dispatch the watchdog observes the
         outer `codex exec --json` stream, where a command's stdout arrives as
         `aggregated_output` on ONE `item.completed` event emitted when the
         command FINISHES; there is no incremental output event. `-v` changes
         what that event contains, never when it arrives, so the 138s of
         silence is identical with or without it.
         I verified the claim rather than deferring to it: no item.started /
         delta / incremental type exists anywhere in the codex path, and the
         lease renews on ANY line from the agent process, so no line means no
         renewal. The reviewer was right.
         `-v` is KEPT (it helps direct execution and the mechanical acceptance
         rerun), but the honest fix for a dispatched long command is a larger
         stall_timeout on that binding -- the silence is structural and cannot
         be removed. Guide corrected: WHERE the output appears matters more
         than whether it exists.
         This is the class of bug only a real run finds. Every unit test
         passes; the step itself passes in 138s standalone. It fails only when
         a long quiet command meets a watchdog under load.
         TERMINAL VERDICT: DEAD 9/12, race=done reclaims=2. The step SURVIVED
         and passed on its third claim, which confirms the diagnosis rather
         than merely being consistent with it -- a genuinely broken step would
         have exhausted its retry budget instead of completing. DEAD (not
         COMPLETE) is the honest outcome: lint-internal failed, so claims and
         e2e stayed correctly blocked and the plan reports no runnable work.

- [x] DONE. Reclaim reasons are durable now (PR 322, un-hashed deliberately:
      guard 9 reads hashes as open-PR citations).
      The reason was never MISSING -- it was computed and then DROPPED.
      ClassifyFailure already turns a stall into FailureStall, but the
      orchestrator writes it via MarkFailedWithPayload, which returns
      ErrTaskNotOwnedRunning once the reaper has reclaimed the task, and that
      error is deliberately swallowed as benign. So exactly when a reclaim
      happens, the category is discarded a few lines from where reclaim_count
      is incremented. Finding that changed the fix: it is a plumbing gap, not a
      missing classifier.
      The reaper now emits one task.reclaimed event per task with plan_id,
      task_id, count, and the branch that fired -- stale_heartbeat vs
      orphaned_claim, already distinct in the SQL and previously collapsed.
      TWO lessons worth keeping:
        - A comment claimed UPDATE...RETURNING was not portable on the pinned
          modernc driver, so the code settled for a summary row. Re-tested on
          v1.54.0: it works. Verify a constraint before designing around it.
        - My first version derived the reason IN the RETURNING clause, which
          evaluates against the POST-update row where claimed_by_worker_id has
          just been nulled -- labelling every reclaim orphaned_claim. The test
          caught it. A test asserting merely that "a reason exists" would have
          passed; asserting the SPECIFIC branch is what caught it.
      Both fixes carry negative proofs.

- [x] DONE (PR 323, un-hashed: guard 9 reads hashes as open-PR citations).
      The task row now names its own reclaim cause:
        race             running    — reclaimed 2x: stale_heartbeat
      Read from the newest task.reclaimed event rather than a durable column,
      which is the right trade: a reclaim is a RECOVERABLE interruption, so the
      task returns to pending and has no terminal state to hang a reason on.
      Deliberately NOT status-gated, unlike the failure reason directly above
      it in the renderer -- a reclaimed task may be running again by the time
      anyone looks, and the count shows in every state.
      MAX(id) not MAX(occurred_at): events are second-resolution, so two
      reclaims within a second tie and the tie-break would surface the OLDER
      reason exactly when reclaims are rapid.
      Query plan checked rather than assumed (this session's own lesson): both
      events lookups report SEARCH ... USING INDEX events_task, so cost is
      per-task constant, not proportional to the events table.
      Proven by reverting at BOTH layers -- dropping the SELECT column fails
      with an empty reason, dropping the render line fails with a bare row. The
      render test reads the PRINTED OUTPUT, not the struct field: a reason that
      reaches --json but never the human line would leave the CLI's default
      output less informative than its own JSON.

- [x] DONE. Acceptance failures now name findings whose paths are not on disk,
      pointing at the tool cache as the likely cause.
      REPORTS rather than suppresses, deliberately: the command still failed
      and the step still fails. Turning a red step green on a heuristic would
      be far worse than the ambiguity being fixed -- a mis-parse would hide
      real findings. This only explains a failure that was happening anyway.
      THE LESSON, which is this session's recurring shape one more time: my
      first version skipped RELATIVE paths, reasoning they resolve against the
      tool's working directory. But checkCommandExitsZero sets cmd.Dir itself,
      so that directory is known -- and golangci-lint reports the real
      stale-cache paths relatively ("../.worktrees/..."). So the detector
      missed the exact output it was written for while PASSING against my
      synthetic absolute-path fixture: a check that works only on its own test
      data. Caught by running it against the REAL captured output instead of
      trusting the fixture. Detecting 2 of 3 real paths is what exposed it.
      Both directions proven by reverting; a companion test asserts a failure
      whose paths all exist is left untouched.

- [x] DIAGNOSED, and the obvious theory was WRONG. I had already written
      "cold compilation dominates" into the guide before measuring it.
      Same machine: 30s warm, 62s COLD-CACHE, 138s UNDER CONCURRENT LOAD,
      against a 180s lease. A cold-but-idle run has ~2x headroom, so the cache
      is not the cause. The original 138s was measured while a full self-test
      ran on the same machine -- which is exactly the condition a dispatched
      self-test creates for ITSELF: every step running in parallel makes this
      one slower.
      So the step is not slow, it is slow WHEN CONTENDED. That is the only
      theory that explains why it passes comfortably by hand and still loses
      two claims on every real run, and why no threshold derived from a quiet
      machine would have predicted it.
      Splitting was evaluated and rejected on measurement, not taste: the
      -race compile is ~1s warm and the run spreads across many tests whose
      slowest is 1.85s. There is no seam. stall_timeout in STORED config is
      the honest remedy (the headless supervisor has no --config-file to
      thread), now documented with the checked duration shape and bounds.
      Cost of nearly shipping the tidy story: one command.

- [x] DONE. A reclaim now names the load that caused it, on all four surfaces
      in one commit:
        race    running    — reclaimed 2x: stale_heartbeat (6 claims in flight)
      concurrent_claims is captured BEFORE the UPDATE releases the claims --
      afterwards it is unrecoverable, since step 2 deletes the workers. Same
      pre-capture discipline the reason needed, for the same reason.
      Gated on `> 1`, not `> 0`, and the difference is load-bearing: a solo
      reclaim records 1 (the task's OWN claim), so `> 0` would print
      "(1 claims in flight)" on every reclaim ever. The idle-case test is what
      catches that -- a detector that always fires passes every presence check.
      The store test asserts the exact value (3), not mere presence, for the
      same reason.
      ALL FOUR SURFACES IN ONE COMMIT, deliberately: the previous change in
      this branch shipped ReclaimReason to store/observe/CLI and left both
      views blind until a reviewer caught it -- on the very change whose test
      file says "a field is not shipped until each renderer shows it". Not
      repeating that two commits later.

- [x] VERIFIED END TO END. Run terminal at 9/12, DEAD only because unit-orch
      failed (correctly blocking claims + e2e). race reached done with reclaim_count=0, and
      EVERY task in the run shows 0 reclaims. Prior runs: 2 unbounded, 6 capped.
      This is the proof that -v, capping, and stall_timeout all failed to
      produce -- each passed its unit tests and changed nothing on a dispatched
      run.
      Four explanations were needed (silence under load, the progress lease,
      dispatch contention, then the acceptance budget) and the first three ALL
      FIT THE DATA. The one that survived is the one whose fix changed the
      observed behaviour. Fitting the observation is necessary, not sufficient.
      REAL ROOT CAUSE, and every earlier story was wrong. A reviewer pointed
      out the fact that breaks them all: runWithHeartbeat beats every 20s
      INDEPENDENTLY of provider output, against a 90s stale window. A stalled
      turn keeps beating, so the watchdog killing a silent turn can never
      produce a reclaim. The lease story cannot explain these reclaims at all.
      What actually happens, traced: the heartbeat goroutine stops the instant
      fn returns (orchestrator.go:297), and the post-run path -- including
      ACCEPTANCE VERIFICATION -- then runs under persistCtx, a 30-SECOND budget
      (orchestrator.go:1995). The race step's acceptance command is
      `go test -race -v ./internal/store/`, measured at 30s warm and 138s under
      load. It cannot fit. persistCtx expires, the task is never marked, and it
      sits `running` with a dead heartbeat until the reaper takes it at 90s.
      So the reclaims are a task whose WORK ALREADY SUCCEEDED being requeued
      because verifying it outlived a budget nobody sized for re-running tests.
      That also explains what the lease story never could: why capping made it
      WORSE (slower machine -> slower acceptance -> more certain to exceed 30s),
      and why race reclaims with nothing else running.
      FIXED in PR 331 (un-hashed), and it took TWO changes because the test
      found the second after the first landed:
        1. verification gets its own detached budget (10m), not the caller's
        2. everything AFTER verification -- the writes recording its verdict --
           gets a fresh detached persist budget too. Detaching verification
           alone was not enough: a slow-but-passing command legitimately
           consumes the caller's whole budget, so MarkDone then failed with
           "context deadline exceeded" and the task stayed unmarked. Same
           lost-verdict bug one step later. A verdict that cannot be WRITTEN is
           indistinguishable from no verdict, which is what the reaper requeues.
      Both proven independently by reverting.
      The bare 30*time.Second literal at four sites is now named persistBudget,
      with verificationBudget beside it -- an unnamed constant is part of why
      nobody noticed acceptance was charged to a store-write budget.
      TEST LESSON worth keeping: my first version of the regression test passed
      a context.Background(), so it PASSED against the unfixed code -- there was
      no caller deadline for verification to outlive. A test for a deadline bug
      must supply the deadline. Caught only by running the negative proof and
      getting no failure.
      Verify by reverting: a dispatched run must reach race=done with
      reclaim_count=0. Nothing short of that has settled this yet -- three
      previous "fixes" (-v, capping, raising stall_timeout) all targeted
      mechanisms that were not the cause.
      INTERIM (11:05): still ZERO reclaims across all 12 tasks, where prior
      runs had 2 (unbounded) and 6 (capped) by this phase. unit-orch failed
      interactive_prompt with a CLEAN TREE and its acceptance command passing
      by hand -- that is the separate pre-existing pattern already queued, not
      a reclaim and not caused by my uncommitted edits as in the earlier run.
      INTERIM (10:59): build reached done with reclaims=0 after a ~7min turn,
      and the run is at 1/12 with 4 in flight and ZERO reclaims across every
      task. Under the old code this is the phase where they accumulated. Not
      yet the verdict -- race has not finished -- but the first phase that
      previously failed has now passed.

- [x] DONE and VERIFIED END TO END. PR 339 merged, and the delete deferred there
      is now proven against a live supervisor:
        200 task rows -> 194, has_more True -> False (page no longer saturated)
        19 plans -> 18; the deleted plan returns not_found
      So the retention primitive works through every layer it was missing:
      store -> IPC -> supervisor -> CLI. The earlier "unknown command:
      plan-delete" was a stale supervisor binary, not a wiring gap -- worth
      noting because that error looked like a defect and was a restart.
      Original framing: CmdPlanDelete through the client, server
      dispatch, supervisor handler, and a `plan delete <id> --yes` subcommand.
      End-to-end delete against a live supervisor is still unverified -- the
      running one predates the binary and correctly answered "unknown command:
      plan-delete", and a self-test run was in flight that a restart would have
      killed. Verify after merge.
      That work also exposed a REAL HAZARD worth remembering: ipc.DriveHandler
      is an OPTIONAL interface detected by type assertion, so adding a method
      silently removed EVERY drive command from the test fake -- plan-import,
      task-approve, worker-kill -- with no compile error. The same slip against
      the real Supervisor would have shipped and disabled the whole drive
      surface at runtime. Now guarded by a compile-time assertion.
      store.DeletePlan HAD NO CALLERS and no CLI surface. Second unwired
      subsystem this session, in code I reviewed earlier without noticing.
      Live consequence: the operator task page saturates at
      MaxOperatorPageLimit (200) and each self-test run adds 12 tasks, so after
      ~16 runs the newest run shows PARTIALLY -- observed 200 rows across 19
      plans, current run contributing 6 of 12. Nothing can prune.
      SWEEP DONE: 75 exported *Store methods, 17 with no production caller,
      splitting into two kinds -- the distinction is what makes it actionable:
        SUPERSEDED (a live replacement carries the traffic) -- DELETE:
          ClaimNextReady->ClaimTask; CreatePlan/AddDep->createPlanOn via
          ImportPlan; MarkFailed->MarkFailedWithPayload;
          HeartbeatWorker->HeartbeatWorkerAndSession;
          ReadOperatorTaskDetail->ListOperatorTaskDescriptions
          SetProjectConfig -> ApplyProjectConfig (a one-line delegate; the
            live path calls ApplyProjectConfig from supervisor/config.go and
            vconfig/source.go)
        UNWIRED (needed, nothing reaches it) -- WIRE:
          DeletePlan, Backup, ReserveTaskInput/Output, the calibration trio,
          ListTaskEvents, ListProjectEvents, ListMessages,
          RecordTaskProviderSession, MaxEventID, CountRunningWorkers,
          ListTaskGroupPaths, TeamRollups
      The list was WRONG TWICE and a reviewer caught both: SetProjectConfig was
      filed UNWIRED when it is a pure delegate, and ListProjectEvents,
      ListMessages and RecordTaskProviderSession were omitted entirely -- my
      first pass filtered out internal/store callers and I never re-ran the
      classification after narrowing the query. Re-derive the list rather than
      trusting this one.
      CAUTION: my first calibration was WRONG. I assumed ClaimNextReady and
      CreatePlan had to be live because dispatch cannot work without them, and
      treated the zero count as proof the sweep was broken. Dispatch uses
      ClaimTask and createPlanOn. Check the replacement before concluding.

- [x] FIRST FULLY GREEN SELF-TEST: COMPLETE 12/12, zero reclaims, zero
      failures, including unit-orch AND race. Read with the new
      `status --plan` filter, which returned all 12 rows where the unscoped
      page showed 0.
      The acceptance-budget fix holds across a whole run, not just the one step
      it was diagnosed on.
      The decision log recorded NOTHING -- correctly, since nothing failed. So
      that test is UNEXERCISED, not passed: the next failure is still the first
      real read of it. Do not mistake a green run for having verified the
      diagnostic.

- [ ] [WAIT] unit-orch is INTERMITTENT and currently PASSING, so there is
      nothing to act on until it fails again. Not closed: an intermittent bug
      that stops reproducing is hidden, not fixed, and the decision log (wired
      in 336) has never actually captured one -- the run that would have
      exercised it came back fully green.
      When it next fails, read the worker.decision_log event FIRST. That is the
      artifact this whole thread lacked.
      Detail: unit-orch fails `interactive_prompt` INTERMITTENTLY -- failed twice, then
      PASSED on the third clean-tree run (verified by reading the store
      directly; see the surface problem below). So it is flaky, not
      deterministic, and an earlier revision of this item calling it
      "reproducible" was wrong. Closed by evidence, do not
      re-litigate: not a timeout (acceptance passes by hand in 11.6s), not a
      retry bug (one claim then terminal -- interactive_prompt is deliberately
      non-retryable, "the CLI is asking for an operator, not another turn"),
      not the stale-linter-cache story (nothing is failing for it to fix).
      The decision log is WIRED as of PR 336 (merged): Ralph records its own
      classification at all four failure sites and absorbs it into a
      worker.decision_log event. So the next unit-orch failure should finally
      produce something readable -- that is the next read, and the first time
      this question has had an artifact to answer it. An intermittent failure
      makes that MORE valuable, not less: it cannot be reproduced on demand, so
      the record has to be captured when it happens.

- [x] FIXED in PR 340: `status --plan <id>` returns a run's full task list
      regardless of history depth. Verified against the live saturated page --
      unscoped showed 0 of 12 rows, scoped returned all 12 -- and used to read
      the fully green run above. SnapshotQuery already carried PlanID and
      `messages` already had the flag; only `status` did not.
      Original finding: `status` CANNOT SHOW A RECENT RUN once the task page saturates, and the
      advice I wrote for it does not work. Verified this pass: a completed
      12-task run displayed 6 rows; there is no `--plan` filter on `status`
      (only --task-after/--task-after-plan cursors), and cursoring cannot
      isolate one plan because other plans' rows consume the page first. I had
      to query SQLite directly to see the run's real state.
      So the guide's "read this run by plan id" is the same defect as the
      "radioactive_ralph plan delete" line it replaced: advice naming a
      capability the CLI does not have. Both were written without trying them.
      FIX: a plan filter on `status` (and the observe query behind it), so the
      operator surface can answer "how did THIS run go" independent of history
      depth. Pruning via DeletePlan helps but is not the same thing -- a filter
      works even when history is legitimately deep.
      Verify by reverting: with a filter, a saturated page must still show all
      12 rows of one run; without it, 6.
      Verify by reverting: a dispatched run must reach unit-orch=done. Its
      command passes standalone, which has never predicted dispatched behaviour
      anywhere in this investigation.

- [x] CAPPED-WIDTH EXPERIMENT CONCLUDED; PR 329 MERGED. Result: Final: race reclaimed FOUR
      times under RALPH_MAX_PARALLEL=4, versus two unbounded -- capping made it
      WORSE. Four successive readings (predicted 0, saw 1 "halved", saw 2 "no
      better", final 4) and only the last is a result; three were recorded as
      conclusions and all three were wrong. A running experiment has no verdict
      until it stops.
      race carries NO pressure clause (gated on >1 in flight), so its reclaims
      happened with nothing else running. SUPERSEDED: this said the LEASE was
      the operative limit; it is not, and the root-cause item above has the
      real answer (acceptance verification charged to a 30s store-write
      budget). Capping also cannot add reclaim exposure by making a step wait
      -- the slot is acquired BEFORE the claim, and only `running` tasks are
      reclaimable.
      Original framing, kept for the diagnosis path:

- [x] [WAIT-AGENT] Capped-width self-test RUNNING (monitor bq7tfrdz1), the
      end-to-end proof the -v fix failed to deliver.
      ANSWERED already: dispatch width is RALPH_MAX_PARALLEL, and unset means
      UNBOUNDED -- supervisorMaxParallel returns 0, the semaphore is nil.
      Verified directly (unset -> 0; "4" -> 4) and confirmed on the live
      process: the supervisor that produced every reclaim this session was
      running with no cap on a 16-core machine. So the contention starving
      `race` was never a tuned budget it lost against; nobody chose 6.
      Documented in the guide, with no recommended optimum -- there is none to
      name, and supervisorMaxParallel's own comment says neither mode is
      adaptive. The point is that the width becomes a DECISION.
      NOW TESTING: supervisor restarted with RALPH_MAX_PARALLEL=4 (confirmed
      in its startup log), tree clean so no uncommitted edits pollute the run
      as they did last time. The claim under test is narrow and falsifiable:
      race should reach done with reclaim_count=0, where it burned 2 on both
      prior unbounded runs.
      If it still reclaims, the contention theory is WRONG and the remaining
      explanation is stall_timeout being too short for this step regardless of
      load -- which is a different fix, so the outcome decides it either way.
      MECHANISM CONFIRMED mid-run, independent of the outcome: with the cap at
      4, SEVEN tasks were dependency-eligible and exactly FOUR ran. The cap is
      genuinely binding, not coincidental -- unbounded, all eleven would have
      dispatched at once.
      RESULT: MY PREDICTION WAS WRONG, and I recorded a WRONG CORRECTION first.
      Predicted reclaim_count=0 under a cap. Mid-run I saw race at 1 reclaim,
      wrote "capping HALVED it", and shipped that. Then race hit 2 -- the same
      count as unbounded. Reading a running experiment at one moment and
      writing the trend as a conclusion is its own error, distinct from the
      original wrong prediction.
      FINAL, from captured `status` output rather than paraphrase:
        race           running  — reclaimed 2x: stale_heartbeat
        unit-provider  failed   — ... — reclaimed 2x: stale_heartbeat (3 claims in flight)
      The MISSING pressure clause on race is the actual finding. It is gated on
      >1 in flight, so race's latest reclaim happened with NOTHING ELSE
      RUNNING. Contention cannot explain that one.
      SUPERSEDED -- see the root-cause item above. Left for the diagnosis
      trail, not as a conclusion. What this said: for a step like this the LEASE is the operative
      limit, not the load. A 138s silent command cannot reliably survive a 180s
      renewable lease even alone on the machine. Capping helps its NEIGHBOURS
      (unit-provider reclaimed at 3 in flight) but does not make a silent step
      visible.
      A reviewer also caught that I FABRICATED the earlier evidence block --
      `reclaims=1 reason=... inflight=4` is emitted by no renderer; it was my
      paraphrase of JSON field names presented as captured output. Fixed with
      real output. Same defect as every other one this session: I wrote what
      the surface would plausibly say instead of running it and reading it.
      Also shipped from this finding (PR 324, un-hashed): --supervisor help now
      names RALPH_MAX_PARALLEL and says the unset default is UNBOUNDED. It was
      documented only in docs/design/, and an operator debugging a reclaimed
      task reads --help.
      Two bot findings on 324 were FALSE POSITIVES (maxParallelEnv "undefined"
      -- it is declared in the same package, go vet is clean, CI compiled it on
      every platform). Countered rather than accepted: the suggested literal
      would have made the test WEAKER, since referencing the constant is what
      makes the assertion follow a rename. A reviewer analyzing a file in
      isolation cannot see its package.

- [x] PRs 327 (attempt accounting) + 328 (the policy decision) MERGED. main has
      Task.AttemptCount, the reclaim-reason surface, and the retry-budget
      policy. (Both were briefly marked [x] BEFORE merging -- a reviewer caught
      that, and the label was restored until they landed. Kept as history: a
      premature [x] tells the loop to skip reconciling work absent from main.)
      A RECLAIMED TASK GETS A FRESH RETRY BUDGET, and the row hides it.
      Found by chasing two steps that failed `interactive_prompt` while their
      acceptance commands pass by hand -- the event history is what actually
      explained it, not the category:
        08:30 claimed / 08:35 claimed / 08:43 failed_terminal (budget exhausted)
        09:23 claimed / 09:37 RECLAIMED / 09:38 claimed / 09:46 RECLAIMED
        09:46 claimed / 09:49 failed_terminal (budget exhausted)
      FIVE claims, TWO reclaims -- and `retry_count` reads 0.
      Mechanism, verified in code not inferred: the reaper (reaper.go) never
      touches retry_count; only MarkFailed does (tasks.go:630). And requeue
      deliberately CLEARS failure_category, so a task that fails, requeues, and
      later exhausts its budget shows whichever category the FINAL attempt had
      -- which is why the row said interactive_prompt while the terminal event
      said "retry budget was exhausted". The row and the event disagreed and
      the event was right.
      Two distinct problems, and they want different fixes:
      (a) TRUTHFULNESS -- retry_count=0 on a task attempted five times is a
          number that means something other than what it says. Either count
          reclaimed attempts or rename what the column reports.
      (b) POLICY -- should a reclaim restore the full budget? Arguably yes (the
          worker died, the task never got its turn) but ONLY if that is
          deliberate. Right now it is a side effect of the reaper not knowing
          about retries, which is not the same as a decision.
      Verify by reverting: a task reclaimed N times must show an attempt count
      an operator can reconcile with its event history. Today those two sources
      disagree, and only the event log is correct.
      NOT the earlier stale-cache story: nothing is failing for these agents to
      try to fix, and their criteria pass right now.

- [x] GUI FLAKE FIXED (PR 325, un-hashed). Chasing a red check on the
      release-please PR found a fixed 3-SECOND deadline for a headless GUI to
      paint its first frame: failed on ubuntu-latest, passed 3/3 locally.
      The release PR's diff is manifest + CHANGELOG only -- no Go code -- so it
      could not have caused the failure it was blocking. Worth checking BEFORE
      assuming a red check belongs to the PR it appears on.
      Same defect this session keeps finding, now in a fourth place: a
      threshold measuring the MACHINE rather than the code. Fixed all 39
      positive waits, not just the failing one -- 38 siblings carried the
      identical flaw waiting their turn. The two NEGATIVE assertions keep
      their 50ms budgets, where the short timeout IS the assertion and raising
      it would silently weaken the test into always passing.
      Proven load-bearing by shrinking the constant to 1ns.

- [x] CONFIRMING SELF-TEST RUN validated the stale-cache diagnosis. The whole
      point of re-running: `lint-internal` had failed with interactive_prompt,
      I traced it to golangci-lint resurrecting findings for files that do not
      exist, cleared the cache -- and on the next run lint-internal came back
      DONE. Prediction made, then tested, rather than asserted.
      `race` also ran with reclaim_count=0 through the same phase that
      previously cost two reclaims.
      (The run showed unit-orch/unit-provider failing, which is MY uncommitted
      work-in-progress being picked up mid-run, not a regression -- the
      self-test reads the working tree. Worth noting because a failed step in
      a dogfood run is not automatically a product finding; check what changed
      under it first.)
      TERMINAL: DEAD 8/12, lint-internal=done, race=done/2. Both predictions
      settled: the stale-cache fix HELD (the step that died on
      interactive_prompt now passes), and race still burned 2 reclaims WITH
      `-v` in place, exactly as the codex reviewer said it would. A prediction
      that survives and one that fails, from the same run.

      Historical note -- the `race` finding cost a
      real diagnosis (2 reclaims read as a stuck row) for a step that was
      merely quiet, and NOTHING in the operator surface said so: the row shows
      reclaim_count, but not WHY the claim was lost. A stall-killed turn and a
      crashed worker look identical after the fact.
      Make the reason durable the way failure_category already is for failed
      tasks -- a reclaim should record what ended the previous claim (stall,
      turn deadline, worker death), so `reclaim_count: 2` becomes readable
      instead of merely alarming.
      Same shape as the interactive_prompt observation above: the invariant
      WORKED, but the operator had to go read watchdog source to learn that.
      Verify by reverting: with the reason recorded, a stalled step names its
      cause; without it, the row is indistinguishable from a dead worker.

- [x] #320 MERGED. Absence assertions must prove presence first -- the third
      distinct form of one mistake this session (a grep matching nothing, a
      timing bound measuring runner speed, a fixture creating nothing). The
      audit also found the closest analogue was NOT broken, which is why it was
      checked rather than "fixed".

- [x] #319 MERGED. store.DeletePlan, and a review caught that it left EVENTS
      behind -- that table carries plan_id with no FK, so the cascade missed
      the row class that actually grows (2 before, 2 after). My test could not
      have caught it: the fixture never ran a task, so the plan had no events
      and the assertion would have passed vacuously. Both fixed; the fixture
      now asserts a non-zero pre-delete count so the check cannot go hollow.

- [x] DONE 2026-07-29: store.DeletePlan -- the retention primitive the
      accumulation analysis said was eventually required. Verified before
      writing it that the cascade would actually fire: every dependent table
      cascades through tasks, and store.go enables foreign_keys per connection.
      Tests pin the parts worth doubting: the deleted plan leaves the operator
      snapshot (removal that leaves the row visible buys nothing over
      archiving), a NEIGHBOURING plan survives, no orphan tasks or task_deps
      remain, and an unknown id ERRORS rather than reporting success -- a prune
      that accepts a typo is worse than one that fails.

- [x] DECIDED 2026-07-29 (not pending work -- a recorded scope boundary): no
      CLI surface for delete. That
      needs a new IPC command, a supervisor handler, and a drive-API method,
      which is a much larger change than ~16 runs of headroom justifies today.
      The store primitive is the load-bearing half; wiring it up is mechanical
      once someone actually needs to prune. Do NOT solve accumulation by
      reverting unique slugs -- that is what stopped the self-test reporting
      stale results.

- [x] #318 MERGED (report() pages like the watch loop, so a run never omits
      its own rows; accumulation analysis corrected -- paging defers rather
      than solves, and a delete path is eventually required).

- [x] DONE 2026-07-29 (#318): self-test runs ACCUMULATE, one plan per invocation.
      Nine runs in a single session took this project to 11 plans / 110 tasks.
      Nothing is degraded yet (`status` returns in ~57ms and the plan page is
      not full), so this is a real trend rather than a live defect -- worth
      fixing before it bites, not urgently.
      The unique-slug design is deliberate and should NOT be reverted: it is
      what stopped the self-test reporting stale results, which was a genuine
      false-green. The question is retention, not uniqueness.
      ARCHIVING IS A DEAD END, checked before proposing it: the operator plan
      query filters only on project id and the keyset cursor -- there is NO
      status filter, so an archived plan still fills a page slot. Setting
      PlanStatusArchived would change a label and nothing else.
      So the real options are: (a) a delete path for old self-test runs, which
      is durable state removal and wants its own design; or (b) leave the runs
      and let `status` page -- the script already passes --plan-limit 200 and
      fails loudly if its own run is missing, so the operator surface degrades
      into noise rather than into a wrong answer.
      (b) is defensible: a wrong answer was the original bug, and noise is not
      that. Prefer it until someone actually trips the limit.
      CHOSE (b) and shipped the one thing it needs: report() was still using
      the DEFAULT 50-plan page while the watch loop paged to 200, so the final
      report would be the first thing to stop showing the run it had just
      started -- silently, since a short page looks identical to a small
      project. Both paths now page to 200. Verified by name: the newest run's
      slug appears in its own report.
      Accumulation is allowed to become noise. It must not become a report that
      omits its own subject.
      CORRECTION, after a review pushed on it: "let it page" is NOT durable.
      MaxOperatorPageLimit is 200 and that is a hard ceiling, so at ~12 tasks
      per run this buys roughly 16 more runs, not indefinite headroom. What
      makes it acceptable in the meantime is that truncation is LOUD -- status
      prints "… more tasks available; showing the first bounded page" -- so the
      surface degrades into a visible short page rather than a silent omission.
      A delete path is therefore not optional, only not-yet-urgent. Whoever
      takes it: the operator plan query has no status filter, so archiving does
      nothing; it has to remove rows.

- [x] #317 MERGED (docs/guides/self-test.md, plus the rule that docs claims
      need the same proof as code claims -- four invented mechanisms in that
      guide were caught by review after I reported it verified).

- [x] DONE 2026-07-29: the self-test was documented NOWHERE a newcomer would
      look -- not the README, not any guide, only the plan file and AGENTS.md,
      both of which you must already know about. A self-test nobody can find
      does not get run, which makes the whole harness decorative.
      docs/guides/self-test.md now covers it: how to run it, why every step
      carries `accept:` (and that a step without one verifies nothing), how to
      read a live run's markers, that a run MUTATES the working tree in two
      ways, and the two step-sizing rules. Linked from the guides index,
      toctree, and README.
      Every factual claim in it was checked against the code rather than
      written from memory -- the gitignore prefixes, the acceptance-marker
      invariant (via its test), and the --watch flag.

- [x] #316 MERGED. The tracked-edit guard now runs from an EXIT trap, so a run
      that FAILS still reports the worker edits it left behind -- found by
      running the script end to end, not by the isolated test, which passed
      throughout. A review then caught that the guard's own test DESTROYED
      uncommitted work (unconditional `git checkout --` on its victim);
      reproduced, then fixed by saving and restoring exact bytes. Verified from
      merged main: 5 checks pass AND an uncommitted marker survives.

- [x] DONE 2026-07-29: the tracked-edit guard only ran on the SUCCESS path.
      `set -e` plus a failing import (no supervisor, bad plan, slug conflict)
      exits before report() is reached -- and a run that died partway is
      exactly the one likely to leave a half-finished worker edit behind, with
      nobody told. Found by running the script end to end rather than trusting
      the isolated test, which passed throughout.
      Fixed with an EXIT trap. The first attempt silently did nothing: the trap
      referenced report_tracked_edits, declared 18 lines LATER, so bash called
      an undefined function. Moved the definition above the trap.
      Two of my own test setups were also wrong before the real defect showed:
      editing the victim file BEFORE the run (so it was captured into the
      baseline), then racing a 1s background edit against an import that fails
      in under a second. Check 5 is deterministic -- a fake binary that edits a
      tracked file and exits nonzero.

- [x] #315 MERGED. The tracked-edit guard now has a behavioural test (4 checks,
      run from a scratch git repo, sourcing the real functions). It found TWO
      blind spots in the guard along the way, both from asking "which state
      does the test never put it in": an already-dirty file, then a STAGED
      edit -- the state one keystroke from committed.

- [x] DONE 2026-07-29: the tracked-edit guard had NO test. CI shellchecked
      scripts/self-test.sh, which proves it parses, not that the guard fires --
      and three separate bugs had already shipped in it (an undefined
      $REPO_ROOT that would crash every run, a baseline captured after staging,
      and a status comparison blind to an already-dirty file).
      scripts/ci/test_self_test_tracked_edit_guard.sh now exercises it in a
      scratch git repo: silent on a clean tree, names a file modified during
      the run, and catches the already-dirty rewrite. It SOURCES the functions
      from self-test.sh rather than restating them, so a copied assertion
      cannot keep passing after the original changes.
      Negative-proofed against both broken shapes: neutering the comparison
      fails check 2, and restoring the old status-based snapshot fails check 3
      with the blind-spot message specifically.

- [x] #314 MERGED. The self-test harness is complete: it warns when a run
      edits tracked source, comparing CONTENT hashes so a file that was already
      dirty is not a blind spot. Verified from merged main in both directions.

- [x] DONE 2026-07-29 (#314): a step EDITED FOUR
      TRACKED SOURCE FILES while trying to make its acceptance command pass.
      internal/agent/echo_unix.go, internal/agent/pty_reader_unix.go,
      internal/orch/contained_open_unix.go,
      internal/provider/result_open_unix.go all gained an
      integer-conversion guard (a new maxUnixFileDescriptor const, gosec G115
      shape). The changes were internally consistent and the tree BUILT -- this
      is the agent doing its job, not a malfunction.
      I reverted them: unreviewed edits to security-adjacent code that appeared
      in my tree without my authorship are not something to adopt by accident,
      however plausible.
      CHECKED, and the finding does NOT reproduce: golangci-lint is clean on
      all three packages. So the agent was speculating -- writing a plausible
      guard for a lint error nobody reported -- rather than fixing a real
      defect. Reverting was right for a second reason.
      HARNESS FIXED 2026-07-29: scripts/self-test.sh now snapshots tracked
      state before the run and reports any tracked file the run changed, naming
      each one. Chose the snapshot over worktree isolation because it keeps the
      run testing the REAL checkout -- isolation would verify a copy, which is
      a different claim -- and the warning is what a reader needs either way.
      THREE bugs while building it, each found by testing rather than reading:
      the guard referenced $REPO_ROOT, a variable this script never defines
      (borrowed from verify-repo-claims.sh), so it would have crashed on every
      run; my first isolation test captured the before-state AFTER staging,
      producing a silent pass that looked like a broken guard; and a review
      caught that comparing `git status` output is blind when a path is ALREADY
      dirty -- " M path" before and after while the content changed underneath.
      Reproduced, then fixed by hashing `git diff` content instead of status
      flags. A guard that misses the concurrent-edit case is decorative,
      because that is the only case it exists for.

- [x] #311, #312, #313 ALL MERGED. The self-test arc is complete:
      scripts/self-test.sh + docs/plans/self-test.md, with acceptance markers
      the orchestrator re-runs, unique slugs per run, exact-slug watching, and
      CI tests asserting the plan parses, that every step is verifiable, and
      that no step depends on a task nothing declares.

- [x] DONE 2026-07-28: a REAL self-test. `scripts/self-test.sh` imports
      `docs/plans/self-test.md` and has Ralph verify Ralph. Two things had made
      every earlier dogfooding attempt useless, neither in the code under test:
      - the plan lived in gitignored `.radioactive-ralph/plans/`, so a branch
        switch deleted it. It is now a tracked SOURCE file in docs/plans/ --
        which does NOT violate the clean-repos contract, because a plan file is
        an input like a Makefile and the state it produces still goes to the
        user-level DB.
      - no step carried an `accept:` marker, so every task was judgment-only,
        accepted on non-empty evidence and failed on empty. Nothing was ever
        verified. Every step now carries a command the orchestrator RE-RUNS.
      Running it for real then found two harness defects: broad sweeps
      (./internal/... ./cmd/...) outlived the provider turn deadline while
      single-package steps passed, and a re-import was refused on slug
      conflict, making the script single-use. Both fixed.
      The run is also the best exercise of the operator surface -- a live plan
      is the only thing producing running workers, real provenance, and fan-out
      partitions at once.

- [x] DONE 2026-07-28: verify-repo-claims.sh attributed a FOREIGN repo's test
      failures to this one. VERIFY_TESTS=1 reported "fyne-lifecycle-ordering
      TESTS FAIL" and "all worktrees build+test NO" -- for a fork of upstream
      fyne that shares the .worktrees/ directory. Three guards (build/test,
      uncommitted, unpushed) globbed every directory there.
      That is the most damaging direction for a verifier to lie in: it sends you
      hunting in the wrong codebase, and the obvious response ("make the tests
      pass") would mean editing an unrelated project. Each loop now checks the
      worktree's remote. Proven in BOTH directions -- accepts this repo, rejects
      the fork, and still fires on a real uncommitted file here. Scoped, not
      silenced, which is the distinction that matters when quieting a check.

- [x] #310 MERGED (flag a plan with no runnable work, plus the forward blocker
      walk and the query-PLAN guard that replaced three failed timing bounds).

- [x] DONE 2026-07-28 by a FIFTH dogfooding pass. With every task row now
      telling the truth, the remaining lie was one level up: BOTH of Ralph's
      own plans reported `status=active task_done=0` while every task in them
      was failed or permanently unreachable. The plan row is the strongest form
      of the stale claim -- an operator scanning a plan list reads "active, 0
      done" as work in progress.
      NoRunnableWork is DERIVED per snapshot, never stored: a read path must not
      mutate durable status, and a dispatcher that later requeues work would
      have to undo it. It reuses the SAME terminal-dependency walk the task rows
      use, since a plan is dead exactly when all its tasks are.
      TWO measurement lessons worth keeping:
      - the empty-plan guard (TaskTotal > 0) never actually fires. A LEFT JOIN
        over zero tasks still yields one row with t.status NULL, and NULL IN
        (...) is not true, so the ELSE arm counts it and runnable comes back 1.
        Kept as belt-and-braces with the measurement written down, and the test
        asserts the OUTCOME so either mechanism changing is caught.
      - my GUI edit silently did not apply (indentation mismatch in the
        replacement). The build passed because nothing changed -- the exact
        false confirmation this session keeps finding elsewhere. Caught only by
        dumping the rendered widget tree.

- [x] #309 MERGED. The transitive blocker walk, plus the review fix that
      replaced its depth cap with visited-node tracking -- a cap does not bound
      that bug, it RELOCATES it past the threshold, and plan import chains
      ordered steps 1:1 with no depth limit so the threshold was reachable
      normally. Cycle termination is now pinned by a test that forces a cycle
      into task_deps directly rather than trusting AddDep's invariant.

- [x] DONE 2026-07-28 by a FOURTH dogfooding pass, run against the merged main.
      The dead-plan marker looked one hop only, so on Ralph's own plan:
        build   failed
        race    pending  -- cannot run: build failed
        parity  pending                              <- silent, and just as dead
      parity depends on race, race is dead behind build, so parity can never run
      either -- but it read exactly like a healthy queued task. Same lie the
      one-hop fix removed, one level deeper, and a real DAG is mostly deeper
      levels.
      Fixed with a recursive CTE that walks the chain and reports the ROOT
      failure (the task that actually died), because naming the intermediate
      would send an operator to another pending row to repeat the lookup. Only
      'failed' terminates the walk -- a merely-unfinished intermediate is not
      traversed, since that chain still clears itself.
      Depth-capped at 64. AddDep prevents cycles, but a projection that can hang
      the operator surface should not depend on that invariant holding.

- [x] GENERATED 2026-07-28 from a P2 review finding on the failure-reason work
      (fixed its two siblings in the same PR; this one is genuinely bigger).
      A failed task whose failure EVENT has aged out of the bounded
      RecentEvents page (default 20) renders as a bare `failed` again. The
      reason lives only in the event log, so a task can sit terminal
      indefinitely while the evidence for WHY scrolls away.
      DONE 2026-07-28 (same PR, after the rendering fixes landed in it):
      migration 0004 adds tasks.failure_category, written on terminal failure
      and CLEARED on requeue so the column always describes CURRENT state. The
      CLI prefers the event summary (prose) and falls back to the durable
      category for the evicted case.
      The schema-version guard caught me adding the migration without bumping
      currentSchemaVersion, with an actionable message naming both fixes -- the
      kind of guard this session has been adding elsewhere, working here.

- [x] #308 MERGED. Grew well past its title: the failure reason on the row, the
      plan-scoped key and status gate two reviews caught, migration 0004's
      durable failure_category, and the TUI/GUI parity fix. Verified on main by
      grepping for the migration, both renderers, and the AGENTS.md rule.

- [x] DONE 2026-07-28 by a THIRD dogfooding pass, run against the merged main
      (the terminal-blocker PR had just landed); shipped in #308. The
      `status` human output renders TASKS but no EVENTS, so a failed task shows
      `failed` and nothing about why. The TUI already renders
      `failure=<category>` (model.go:800); the CLI human path renders no event
      surface at all, so it stays strictly less useful than its own --json.
      TWO CORRECTIONS I made while diagnosing this, both worth keeping because
      each nearly became a wrong fix:
        - I first concluded observe.FailureCategory was "declared and never
          wired". WRONG -- internal/a2a and internal/provider both use it, and
          observe.Event carries `Failure *FailureSummary`.
        - I then concluded failureForEvent did not handle
          `task.failed_terminal`. ALSO WRONG -- it does; I had read only the
          first case of the switch.
      The data is fully populated end to end. Verified in the live snapshot:
      `{"category":"task_terminal","summary":"task retry budget was
      exhausted","retryable":false}`. My earlier greps filtered the field out,
      which is what made it look absent. The gap is ONLY the CLI rendering.
      DONE 2026-07-28 (#308): the failure summary now renders on each failed
      task's row, reusing observe's FailureSummary rather than a second
      taxonomy; newest event wins per task, since a task can fail, requeue, and
      fail again. Verified on the live supervisor.
      One more lesson from the test: my fixture set events without EventCursor
      and ValidateSnapshotResponse REJECTED it ("event 26 exceeds cursor 0").
      That is the snapshot contract failing closed, not a test nuisance.

- [x] DONE 2026-07-28 (#307): rebased onto main after #306 landed and opened.
      Originally recorded as BUILT/waiting. Generated by
      a SECOND dogfooding pass (the first found the partition bug in #306). Reading `status` on a plan whose first task
      FAILED shows the gap plainly:
        build   failed    via=codex
        claims  pending
        e2e     pending
        race    pending
      Those three declare after:[build] and can NEVER run, but they report a
      bare `pending` -- byte-identical to a task that simply has not started.
      An operator cannot tell a permanently-stuck plan from a slow one.
      The data exists (6 task_deps queries across claim.go, tasks.go,
      ready_partition.go, and operator_snapshot.go) and observe exposes it in
      ZERO. Note the last one: the snapshot query ALREADY walks task_deps for
      the readiness gate #306 added -- it just collapses the walk to a boolean
      and throws the edge away. Naming the blocking dependency is a smaller
      change than it looks, not a new join. This is the same shape as the Blocked field: "why is
      this stalled?" is answerable only from raw SQLite, the access the
      dumb-client boundary exists to remove.
      Design constraint, decided and recorded rather than discovered later: do
      NOT project raw edges as a task list -- that is a DAG dump, not an
      answer. Project the ANSWER: for a task that cannot run, name the
      unsatisfied dependency and whether it is merely incomplete (will clear)
      or terminal (will not). The second case is the one worth surfacing
      loudly; it is a dead plan wearing a `pending` badge.
      TERMINAL IS VERIFIED, not assumed. Both readiness walks
      (ready_partition.go:108, operator_snapshot.go:717) satisfy a dependency
      only on 'done', 'skipped', 'decomposed'. MarkFailedWithPayload retries by
      setting 'pending' and, once retries are exhausted, sets 'failed' -- and
      no transition anywhere leaves 'failed'. So a dependent behind a failed
      task can NEVER run, which is exactly what makes the bare `pending` badge
      a lie worth fixing rather than a cosmetic nit.
      BUILT 2026-07-28 on local branch `feat/name-terminal-blocker` (c898799),
      NOT pushed. Field `BlockedByTaskID` projects store -> observe -> TUI/GUI/
      CLI; verified on the LIVE supervisor, where the three dependents of a
      failed `build` now read "cannot run: build failed" and the one whose
      blocker has not failed stays correctly unmarked.
      It is STACKED on #306: both edit internal/store/operator_snapshot.go
      (same query) plus operator_partition_test.go and meso_provenance_test.go.
      Rebasing onto main now would mean resolving conflicts against work that
      is about to land, so: wait for #306, rebase, then PR. Cheaper than it
      looks -- the snapshot query already walked task_deps for the readiness
      gate and discarded the edge, so this is a correlated subquery over rows
      it was already visiting.

- [x] #305 MERGED (CLI task table + blocked-reason in all three views + meso
      row width + GUI worker-claim parity).

- [x] #306 MERGED (partition ordinal gated on partition membership).

- [x] #307 MERGED (name the dependency that makes a task unreachable -- the
      gap the SECOND dogfooding pass found). Verified on main by grepping
      operator_snapshot.go for blocked_by and query_cmd.go for "cannot run".

- [x] SUPERSEDED by the line above: Land the 1 open PR: #306 (partition ordinal gated on partition
      MEMBERSHIP -- the bug DOGFOODING found, plus the regression a review
      found in that fix). Now genuinely a wait: both P1 threads answered and
      resolved (2663efe), zero failing checks, 15 running, auto-merge ARMED.
      The earlier E2E failure was `go mod download` hitting a connection reset
      on fyne.io/fyne/v2 from the module proxy -- infrastructure, not the diff
      (which touches no go.mod, workflow, or e2e file); the new push replaced
      that run.
      Worth keeping: I first labelled this [WAIT] while three actionable items
      existed and guard 8 caught it. A wait-label over actionable state is the
      same false all-clear the guards exist to prevent -- the label has to
      describe the state, not the intention.
      Hash-prefixed deliberately: guard 9 extracts `#[0-9]{3}` from THIS line.

- [x] DOGFOODING DONE 2026-07-28 -- and it paid for itself immediately.
      Authored .radioactive-ralph/plans/observe-surface-followups.md, imported
      it, ran a real supervisor, and read Ralph's own state through the CLI
      table built this session. Two things came out of it:
        - the importer REJECTED my first draft with a precise message
          (narrative paragraph before the list) -- the validator working.
        - `status` showed `build` sharing a partition marker with the three
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
        - TUI width: the worst-case row was 215 columns and wrapped
          mid-sentence. Now ~104 (block reason on a continuation line, worker
          id abbreviated to its distinguishing TAIL -- a first pass truncated
          the head and made every row read "worker-…").
        - GUI clipping: Codex caught it before I did. The remediation was an
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
      - The full consumer chain WAS checked and is sound: IPC uses type
        ALIASES (`type ObserveSnapshotReply = observe.Snapshot`,
        internal/ipc/protocol.go:315), so no field can be dropped there, and
        `query --json` serializes the whole snapshot. No work needed -- recorded
        so the next pass does not re-derive it.
      - REAL GAP: the human-readable `query` output is a single summary line
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
