---
title: Fixit delegation — plan creation pipeline
description: How fixit turns a one-line ask into a structured, DAG-ready plan, and how it hands that plan off to a supervised variant.
---

`fixit` is the only variant that can create plans and recommend peers.
Every other variant refuses to boot without an active plan. This page
walks the six-stage pipeline fixit uses when an operator asks it to
plan a new feature or migration.

## Entry point

```sh
radioactive_ralph run --variant fixit --advise \
  --plan docs/plans/v0-2-stack-redesign.prq.md
```

`--advise` puts fixit into plan-creation mode. It loads the markdown
PRQ (if one exists), assesses what's known vs. unknown, and runs the
six-stage pipeline below. Without `--advise`, fixit executes an
existing plan like any other variant.

## The six stages

### 1. Intent capture

fixit reads the PRQ (or prompts the operator) and writes an
`intents` row into the plan DAG. The intent captures *what the
operator asked for* verbatim — unshaped, ungroomed. This row is the
ground-truth reference the rest of the pipeline compares against.

### 2. Analysis

fixit walks the repo, reads the design docs (`docs/DESIGN.md`,
`docs/ARCHITECTURE.md`, `STANDARDS.md`), and writes an `analyses`
row with its understanding of the codebase's current shape. This is
separate from the intent so we can later detect drift — if the
codebase changed since fixit analyzed it, subsequent stages re-run.

### 3. Initial task DAG

Using the intent + analysis, fixit drafts a first-pass DAG of
`CreateTaskOpts`. This is the "if I had to ship this tomorrow, here
is what I'd do" draft. It's typically coarse — 10-15 tasks — and
each task carries a `confidence` score.

### 4. Confidence threshold + refinement

If any task's confidence is below the configured threshold
(`fixit.confidence_threshold` in config.toml, default 70), fixit
decomposes that task into sub-tasks and re-scores. The decomposition
loop continues until all tasks clear the threshold OR the
`fixit.max_refinement_passes` cap is hit. Either way, fixit writes a
`decomposition` event for each split so the history is auditable.

### 5. Acceptance criteria synthesis

fixit synthesizes acceptance-criteria tasks that depend on all the
work tasks. These are gate tasks — they can't be claimed until every
upstream task is `done`, and they encode "how will we know this is
actually finished" in machine-checkable form (tests pass, lint
clean, CI green, binary builds, etc.).

### 6. DAG commit + variant assignment

Finally, fixit calls `internal/fixit/emit_dag.go::EmitToDAG` which
inserts the plan, tasks, deps, acceptance gates into the plandag
store in a single transaction. Each task's `variant_hint` is set
based on fixit's classification (tear-down tasks → green, security
review → professor, etc.); the operator's bias preferences from
`config.toml` override hints at claim time.

## Handoff

Once the DAG is committed, fixit either:

- **Prints the plan** and exits (default), so the operator can
  inspect it before supervised execution starts. Run
  `radioactive_ralph plan show <slug>` to review, then
  `radioactive_ralph run --variant <name> --detach` to execute.
- **Auto-handoffs** (with `--auto-handoff`), launching supervised
  variants immediately against the freshly committed plan. This is
  the "leave me alone for the weekend" mode.

## Re-advising

fixit can re-run stages 2-6 on an existing plan without recreating
it. Useful when the codebase has moved significantly since the
original analysis:

```sh
radioactive_ralph run --variant fixit --advise \
  --plan docs/plans/v0-2-stack-redesign.prq.md \
  --refresh-analysis
```

This re-writes the `analyses` row, re-scores existing tasks,
decomposes any that fell below threshold, and updates acceptance
gates — all under the existing plan ID, preserving task history.
