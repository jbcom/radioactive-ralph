---
title: Fixit delegation — plan creation pipeline
description: How fixit advisor mode turns a one-line ask into a recommendation report and a durable DAG plan.
---

`fixit` is the only variant that can recommend peer variants and write a
fresh advisory report. Every other variant expects an active plan in the
live store already.

## Entry point

```sh
radioactive_ralph run --variant fixit --advise \
  --topic runtime-stabilization \
  --description "Close the remaining runtime, docs, and launch-readiness gaps"
```

`--advise` puts fixit into advisor mode. It scans the repo, assesses
what's known vs. unknown, writes a recommendation report to
`.radioactive-ralph/plans/<topic>-advisor.md`, syncs that proposal into
the durable plan DAG on first creation for the topic slug, and can
optionally hand off to the recommended variant. Without `--advise`,
fixit behaves as a bounded ROI worker.

## The six stages

### 1. Intent capture

fixit records the operator ask from `--description`, `TOPIC.md`, or
its fallback prompt context. The goal is to preserve the original ask
before any ranking or decomposition starts.

### 2. Analysis

fixit walks the repo, reads the docs and project metadata, and builds
an analysis record of the codebase's current shape. This is separate
from the operator intent so later passes can detect drift.

### 3. Initial task set

Using the intent plus analysis, fixit drafts a first-pass task list and
variant recommendation.

### 4. Confidence threshold + refinement

If confidence lands below the threshold, fixit refines the proposal up
to `--max-iterations`. The point is to get from vague advice to a
report that is actually actionable.

### 5. Acceptance criteria synthesis

fixit synthesizes acceptance criteria for the recommendation so the
operator or downstream variant has a concrete definition of done.

### 6. Report emission, DAG sync, and optional handoff

Finally, fixit writes `.radioactive-ralph/plans/<topic>-advisor.md`.
If this is the first durable plan for that repo/topic slug, fixit also
creates the executable DAG plan. If `--auto-handoff` is enabled and the
recommendation is unambiguous, fixit launches the recommended variant as
a follow-up run.

## Handoff

Once the report is written, fixit either:

- **Prints the recommendation** and exits so the operator can inspect it.
- **Auto-handoffs** (with `--auto-handoff`), launching the recommended
  variant immediately. This is the "leave me alone for the weekend"
  mode.

## Re-advising

fixit can re-run the same topic after the repo changes:

```sh
radioactive_ralph run --variant fixit --advise \
  --topic runtime-stabilization \
  --description "Re-score the remaining runtime work after the latest docs and packaging pass"
```

That re-runs the analysis and refinement loop and overwrites the
advisor report with current context. Today it does not rewrite an
existing durable DAG plan with the same slug; it refreshes the human
report and leaves the stored plan in place.
