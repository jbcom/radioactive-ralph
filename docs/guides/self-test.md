---
title: Self-test
description: Have Ralph verify Ralph, using Ralph — the dogfooding entry point.
---

# Self-test

```bash
radioactive_ralph --supervisor &   # in another shell, if one is not running
scripts/self-test.sh               # import and report once
scripts/self-test.sh --watch       # follow until it settles
```

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

```
  build            done       via=codex
  unit-store       running    w:…7f3a2b1c via=codex p1
  unit-orch        running    w:…7f3a2b1c via=codex p1
  e2e              pending    — cannot run: build failed
```

- `via=` — which provider executed the task, surviving the worker itself
- `w:…` — the worker currently holding the claim
- `p1` — a fan-out partition: these tasks go to one provider turn
- `— cannot run: X failed` — this task is unreachable, naming the *root* failure

A plan whose every task is finished, failed, or blocked also reports
`no runnable work`, which distinguishes a dead plan from a slow one.

## A run modifies your working tree

Two things worth knowing before starting one.

**Scratch lands in the project dir, by design.** A contained turn sets `HOME`
and `TMPDIR` under the containment root so its writes cannot escape, and
acceptance commands re-run in scratch trees of their own. These are gitignored
(`.codex-*`, `.rr-accept.*`, `.tmp-*`) and they have to be: their contents churn
fast enough that `git add -A` does not merely stage junk, it *fails* mid-stat on
a file the turn already deleted.

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

## Writing steps

**Size each step to the provider turn deadline.** A step whose turn outlives the
deadline fails for reasons unrelated to the code, and reads identically to a
real failure. Broad sweeps (`go test ./internal/... ./cmd/...`) hit this;
per-package steps do not.

**Partition coverage, never drop it.** When a step grows too slow, split it —
do not narrow what it tests. An early revision cut the unit pass down to one
package, which fit comfortably and would have passed green while a regression
sat anywhere else.

Coverage is maintained by hand; `go list ./...` is the checklist. There is no
test enforcing it — one was written and removed after four attempts, each of
which passed against a plan with a package deliberately deleted.
