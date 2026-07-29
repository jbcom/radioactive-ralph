---
title: Self-test
description: Have Ralph verify Ralph, using Ralph — the dogfooding entry point.
---

# Self-test

```bash
go build -o ./radioactive_ralph ./cmd/radioactive_ralph
./radioactive_ralph --supervisor &   # in another shell
scripts/self-test.sh                 # import and report once
scripts/self-test.sh --watch         # follow until it settles
```

Start the supervisor from the **checkout under test**, not from `PATH`. The
supervisor is what runs dispatch, the watchdog, and acceptance verification —
the runtime being dogfooded. An older installed binary on `PATH` would happily
serve the run, and the self-test would then be verifying a release you are not
changing.

The script imports [`docs/plans/self-test.md`](../plans/self-test.md) into a
running supervisor and has Ralph verify Ralph: build, per-package unit suites,
race, lint, then the end-to-end pty run and the repo's own claim verifier.

## Why every step carries `accept:`

Each step ends with an inline `` `accept: <command>` `` marker, so the
orchestrator **re-runs the check itself** rather than believing the worker.
A step without one is judgment-only — accepted on any non-empty evidence,
failed on empty. The first version of this plan had none, so nothing was ever
verified and the whole run died; the failure looked like a product bug rather
than a plan bug.

That distinction is the entire value of the exercise. `go test ./...` tells you
the code compiles and passes. The self-test tells you the *product* — dispatch,
containment, acceptance verification, the operator surface — actually works on
a real plan.

## Reading a run

`radioactive_ralph status` during a run is the fastest way to exercise the
operator surface, because a live plan is the only thing that produces running
workers, real provenance, and fan-out partitions at once:

A healthy run mid-flight, after `build` verified and its dependents dispatched:

```
  build            done       via=codex
  unit-store       running    w:…7f3a2b1c via=codex p1
  unit-orch        running    w:…4a852dec via=codex p1
  e2e              pending
```

And a run where `build` failed, so nothing downstream can proceed:

```
  build            failed     via=codex — task retry budget was exhausted
  unit-store       pending    — cannot run: build failed
  e2e              pending    — cannot run: build failed
```

- `via=` — which provider executed the task, surviving the worker itself
- `w:…` — the worker currently holding the claim. Two tasks in one partition
  can show DIFFERENT workers, as above
- `p1` — a partition that *may* be coalesced into a single provider turn, when
  the bound provider declares `NativeFanout`. `codex` does not, so these
  dispatch as separate workers and separate turns; the marker says "one worker
  may own these", not "one worker does"
- `— cannot run: X failed` — this task is unreachable, naming the *root*
  failure rather than the intermediate one

**The task page saturates before you notice.** `MaxOperatorPageLimit` is 200 for
tasks as well as plans, and every self-test run adds 12 tasks — so after roughly
sixteen runs the page fills and the newest run is shown PARTIALLY. Observed: 200
rows spanning 19 plans, with the current run contributing 6 of its 12 tasks.

`has_more` reports this honestly; the risk is that nothing reads it. A partial
page looks exactly like a small project, which is the same shape as the
plan-page hazard the script already guards. `scripts/self-test.sh` now warns when
the page is full.

Scope the read to one plan id — **pruning is not available**. `store.DeletePlan`
exists, is tested, and has no callers and no CLI surface, so accumulated runs
cannot be removed. That is the same unwired-subsystem shape as the decision log,
and it is why the page fills with nothing to do about it.

A plan whose every task is finished, failed, or blocked also reports
`no runnable work`, which distinguishes a dead plan from a slow one.

## When a step fails, read the evidence log, not the failure event

A failed task's `task.failed_terminal` payload carries a **closed-set constant**,
and for the most common case that is all it carries -- every generic
`interactive_prompt` failure in this repo's store is the same 89 bytes:

```json
{"reason":"provider requested interactive input","failure_category":"interactive_prompt"}
```

Other categories carry their own fixed summaries, so payload size varies (69
bytes for a cancel, 157 for a permission block, and an acceptance rejection can
be much larger because the acceptance output is not provider prose). What is
constant is the *rule*: `provider.ClassifyFailure` emits a closed set of
privacy-safe strings, never raw provider diagnostics. So a category tells you
which KIND of failure happened and never why.

**The evidence is in `a2a_messages`, not in `worker.completed`.** This matters
because `worker.completed` is written by `store.MarkDone` -- only *after*
verification succeeds -- so for an actually-failed step it does not exist.
Submitted evidence is recorded before verification and survives a failure:

```sql
-- macOS: ~/Library/Application Support/radioactive-ralph/ralph.db
-- Join on the FULL task key: self-test plans reuse task ids across runs, so
-- `t.id = m.task_id` alone can return another run's row.
SELECT m.content_json
FROM a2a_messages m
JOIN tasks t ON t.id = m.task_id AND t.plan_id = m.plan_id
JOIN plans p ON p.id = t.plan_id
WHERE p.slug = '<your-run-slug>'
  AND t.description LIKE '%<package you care about>%'
ORDER BY m.id DESC;
```

That returns the worker's own account of what it ran and what happened -- for
example, one real record from this repo's runs reported `internal/observe` PASS,
`internal/plan` PASS, and `internal/orch` FAIL because three tests reached
`agent.Start()` and got `operation not permitted`, with an independent probe
confirming the sandbox blocked pty creation (`script: openpty: Operation not
permitted`). No failure event could have told you that.

Two traps in that same record:

- **`exit_code: 0` does not mean the acceptance passed.** `a2a.Evidence`
  documents `ExitCode` as **advisory only** -- the orchestrator re-runs the real
  check and never trusts it. In the record above the worker reported `0` while
  its own prose said `FAIL`.
- **A category is only as true as the pattern that assigned it.** Read the
  category, then read what matched to earn it. A bare `permission` pattern once
  matched `permission denied` in ordinary test output and labelled a red test an
  interactive block.

## A red check is often not your diff

Three distinct infrastructure flakes hit this repo's CI in a single day, none of
them caused by the change under test. Read the log before assuming otherwise:

- `hdiutil: create failed - Resource busy` -- a stale DMG device left attached
  by a previous job on a reused macOS runner.
- `sum.golang.org ... stream error; INTERNAL_ERROR` -- the Go checksum database
  dropping a connection mid-download.
- `no output before stall timeout` in `internal/provider` -- exec-to-first-byte
  latency under heavy parallelism, not a logic bug. Do not "fix" these by
  widening `StallTimeout` or reducing parallelism; find what is paying startup
  cost inside the stall window.

## A run modifies your working tree

Two things worth knowing before starting one.

**Scratch lands in the project dir.** Observed during real runs: `.codex-*`,
`.rr-accept.*`, `.tmp-go-*`, `.tmp-race-work.*` and friends. These come from the
provider AGENT choosing to work there — Ralph itself does not set `HOME` or
`TMPDIR`, and the acceptance checker only sets `cmd.Dir` to the project
checkout — so the exact set depends on the CLI and what a turn decides to do.

They are gitignored by prefix for that reason: an enumerated list would go stale
the moment an agent picks a new name. It also has to be prefixes rather than
nothing, because their contents churn fast enough that `git add -A` does not
merely stage junk — it *fails* mid-stat on a file the turn already deleted:

```
fatal: unable to stat '.codex-.../store.db': No such file or directory
```

**A step can edit tracked source.** A provider turn trying to make its
acceptance command pass will change code to do it — during one run the
`unit-client` step rewrote `internal/ipc/ipc_test.go`. That is the agent doing
its job, but it means a run leaves edits nobody authored. The script reports
them on exit, including when the run *fails*:

```
self-test: WARNING — this run modified tracked files:
  internal/agent/echo_unix.go
  A provider turn edits source to make its acceptance pass. Review these
  before committing -- you did not write them.
```

Review those before committing. One run produced a plausible, compiling
integer-overflow guard across four files for a lint error that — checked
afterwards — did not reproduce.

That run's root cause was found later, and it is worth knowing because it makes
the edits look justified: **a stale linter cache invents work.**
`golangci-lint run ./internal/...` reported 11 issues, every one of them in
`../.worktrees/rr-sandbox/` — a sibling directory that is not a worktree of this
repo, has no `go.work` entry, and whose files do not exist on disk. The findings
came from golangci-lint's own cache. After `golangci-lint cache clean`, the same
command reports `0 issues`.

So the `lint-internal` step failed on phantom findings, and the provider turn did
exactly what a diligent agent should: it tried to fix them, could not (the files
are gone), and asked for interactive guidance — surfacing as
`failure_category: interactive_prompt`. Both anomalies in that run trace to the
one cause.

A lint failure naming a path outside the repo is the tell. Check that the file
exists before believing the finding, and clear the cache before concluding the
code is at fault.

## Writing steps

**Size each step to the provider turn deadline.** A step whose turn outlives the
deadline fails for reasons unrelated to the code, and reads identically to a
real failure. Broad sweeps (`go test ./internal/... ./cmd/...`) hit this;
per-package steps do not.

**A silent step is a stalled step.** Two independent bounds govern a turn, and
the SHORTER one is the one that bites: the turn deadline (30m) is generous, but
the progress lease (`DefaultStallTimeout`, 3m) is renewed by OUTPUT. A command
that runs a long time while printing nothing looks identical to a hung provider,
so the watchdog kills it and the reaper reclaims the task.

Observed: the `race` step ran `go test -race ./internal/store/`, which prints a
single `ok` line after 138s of silence. That is 41s of headroom against the
lease -- and under a concurrent self-test run it lost that race twice
(`reclaim_count: 2`) before finishing. Nothing was wrong with the code or the
test; the step was simply invisible while it worked.

**Where the output has to appear matters more than whether it exists.** The
obvious fix — add `-v` so each test line renews the lease — only works when the
watchdog can SEE those lines. It can, for a directly executed command and for
the mechanical acceptance rerun. It cannot under Codex dispatch: the watchdog
observes the outer `codex exec --json` event stream, and a command's stdout
reaches that stream as `aggregated_output` on a single `item.completed` event
emitted only when the command FINISHES. There is no incremental output event.
So `-v` changes what that one event contains, never when it arrives, and a
138s test is exactly as silent to the lease with it as without.

The step still carries `-v`, which is worth having for the paths that can use
it. But when a genuinely long command must run under a dispatching provider,
the silence is STRUCTURAL and cannot be removed — raise `stall_timeout` for
that binding instead (Go duration string, default `3m`, hard maximum `1h`):

```toml
stall_timeout = "10m"
```

A running supervisor reads this from **stored** config, not from a file passed
at self-test time: the headless supervisor has no `--config-file` to thread, so
the value has to be in the DB layers before the run starts.

Splitting the step was tried first and does not apply here, which is worth
recording so nobody re-derives it. The `race` step's cost is not one slow test
that could be peeled off: the `-race` compile is ~1s warm, and the run is spread
across many tests whose slowest is 1.85s. There is no seam to split on.

The timings are worth stating plainly, because the obvious theory is wrong.
Measured on one machine: **30s warm, 62s cold-cache, 138s under concurrent
load** — and the lease is 180s. Cold compilation is NOT what pushes this past
the limit; a cold-but-idle run has ~2x headroom. The 138s figure that started
this was measured while a full self-test was running on the same machine, which
is exactly the condition a dispatched self-test creates for itself. Every step
it runs in parallel makes this one slower.

So the step is not slow, it is slow *when contended* — which is why it passes
comfortably by hand and loses claims during a real run, and why no fixed
threshold derived from a quiet machine would have predicted it.

Remove the silence when a seam exists, raise the lease when it does not.
Reaching for the lease first is how a real hang gets a longer rope.

**Capping the width made it worse.** The prediction was that it would fix this,
so it was measured. With `RALPH_MAX_PARALLEL=4` the `race` step reclaimed **four**
times, against two when unbounded. Captured from `radioactive_ralph status`:

```
  race             running                  w:…124303ac via=codex — reclaimed 4x: stale_heartbeat
  unit-provider    failed                   via=codex — task retry budget was exhausted — reclaimed 2x: stale_heartbeat (3 claims in flight)
```

The absent pressure clause on the `race` row is the strongest single piece of
evidence, but it is narrower than it looks: the row carries only the NEWEST
reclaim's conditions (`operatorTasksQuery` selects it with `MAX(id)`), while
`reclaimed 4x` is cumulative. So it establishes that reclaim #4 happened with
nothing else in flight — it says nothing about the first three.

That one data point is still decisive, because a single reclaim under zero
contention is enough to rule contention out as a NECESSARY cause. It is not
enough to rule it out as a contributor, and the earlier reclaims may well have
had company.

**It is not the lease either.** That was the next wrong answer, and the fact that
kills it is one function away: `runWithHeartbeat` beats every 20s *independently
of provider output*, against a 90s stale window. A stalled turn keeps beating, so
a watchdog kill cannot produce a reclaim at all.

What actually happens:

- the heartbeat goroutine stops the instant the turn's `fn` returns
- the post-run path — **including acceptance verification** — then runs under
  `persistCtx`, a **30-second** budget
- this step's acceptance command is `go test -race -v ./internal/store/`, which
  takes 30s warm and 138s under load

It cannot fit. `persistCtx` expires, the task is never marked, and it sits
`running` with a dead heartbeat until the reaper takes it at 90s. So the reclaims
are a step whose *work already succeeded* being requeued because verifying it
outlived a budget sized for store writes.

That explains what neither earlier theory could: why capping made it worse (a
busier machine makes acceptance slower, so blowing 30s becomes more certain), and
why it reclaims with nothing else running.

Capping also cannot add reclaim exposure by making a step wait for a slot:
`dispatchReadySteps` acquires the slot BEFORE claiming, and `ReclaimStale` only
requeues tasks already in `running`, so a candidate waiting on a full semaphore
is not eligible. All exposure is inside the turn, once the claim is held.

The honest reading is that this experiment produced four successive readings and
three successive WRONG EXPLANATIONS. The readings: predicted 0, saw 1 and wrote
"halved", saw 2 and wrote "no better", finished at 4+. The explanations: silence
under load, then the lease, then — only after a reviewer pointed at the heartbeat
interval — the acceptance budget.

Two lessons, and the second is the expensive one. A running experiment has no
verdict until it stops. And a mechanism that *explains* the observation is not
therefore the mechanism: the lease story fit every number I had, and was still
wrong, because I never checked whether a stalled turn actually stops
heartbeating. Fitting the data is necessary, not sufficient.

What made the difference legible is the in-flight count on each reclaim. Without
it, both rows read as "still flaky" with no way to separate a neighbour effect
from a root cause — `unit-provider`'s reclaims genuinely happened under load,
`race`'s did not.

**Nobody chose the width.** Dispatch concurrency is `RALPH_MAX_PARALLEL`, and
when it is unset the supervisor is *unbounded* — `supervisorMaxParallel` returns
0 and the semaphore is nil. That is the state a self-test runs in by default, so
the six-way contention starving the `race` step is not a considered setting; it
is however many steps happen to be ready at once.

That reframes the fix. The step is not competing with a tuned parallelism budget,
it is competing with everything. Before raising `stall_timeout`, try capping the
width:

```bash
RALPH_MAX_PARALLEL=4 radioactive_ralph --supervisor
```

The guide does not name an optimum, because there isn't one to name: the right
value depends on the machine and on what the plan's steps do. The comment on
`supervisorMaxParallel` is explicit that neither mode is adaptive or recommended.
What matters is that the number becomes a *decision* rather than an accident —
an unbounded default makes every step's timing depend on how many siblings its
dependency graph happens to unblock.

Only that ONE step is anywhere near the lease, which is worth knowing before
changing anything globally. Every other step was measured: the unit suites and
both `golangci-lint` runs finish in 2-13s, and `-race` over the same store
package that takes 2s without it takes 138s with it — ~46x the next-slowest
step. Measure before broadening a fix.

**Partition coverage, never drop it.** When a step grows too slow, split it —
do not narrow what it tests. An early revision cut the unit pass down to one
package, which fit comfortably and would have passed green while a regression
sat anywhere else.

Coverage is maintained by hand; `go list ./...` is the checklist. There is no
test enforcing it — one was written and removed after four attempts, each of
which passed against a plan with a package deliberately deleted.
