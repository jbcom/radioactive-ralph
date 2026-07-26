---
title: Plan format
description: The markdown grammar plans are decomposed from, and how completion is verified.
---

# Plan format

Plans are plain markdown parsed with `goldmark` in pure Go. The default
format is decomposed heuristically; an additive `ralph.plan/v2` task block
can opt a whole plan into an explicit, machine-validated DAG. No LLM is
involved in either decomposition path.

## Grammar

- **A heading of level N is a nesting group.** Its section runs from the
  heading to the next heading of level ≤ N.
- **Heading order encodes group dependency.** `# Do first` followed by
  `# Do next` means the first group's steps complete before the second
  group starts.
- **Under a leaf heading** (one with no child subheadings):
  - an **unordered list** = parallelizable steps
  - an **ordered list** = sequential steps
  - a step may carry paragraphs of supporting detail
- **Don't descend past a heading that has child subheadings** — the
  subheadings carry the ordering, not a list under the parent.
- **A bare paragraph with no list is narrative, not a step.** A list under
  a heading is what makes it a step-group; the validator enforces this
  disambiguation.

## Example

```markdown
# Fix the login bug

- Reproduce the failure locally
- Write a failing test
- Patch the handler

# Ship it

1. Open a PR
2. Wait for CI
3. Merge
```

"Fix the login bug" is a parallel step-group (unordered list); "Ship it"
is sequential (ordered list) and only starts once every step in "Fix the
login bug" is done.

## Explicit task DAGs (`ralph.plan/v2`)

Use v2 when several teams or providers need a durable execution contract
instead of heading/list-order inference. Every list item must contain exactly
one indented `ralph-task` fenced block. Once any step uses the block, every
step must use it; mixed legacy/v2 plans are rejected.

````markdown
# Story

- Draft the story contract

  `accept-file: artifacts/story.json`

  ```ralph-task
  {
    "id": "story.draft",
    "after": [],
    "team": "studio/narrative",
    "binding": {
      "mode": "pool",
      "alias": "",
      "provider": "",
      "model": "",
      "effort": "",
      "calibration": "",
      "repetitions": 0,
      "fixture": ""
    },
    "requires": ["local-agent"],
    "providers": ["claude", "codex"],
    "differentFrom": [],
    "inputs": [
      {
        "path": "docs/creative-contract.md",
        "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
      }
    ],
    "outputs": [
      {"path": "artifacts/story.json", "mode": "exclusive"}
    ]
  }
  ```

- Review the contract independently

  `accept-file: artifacts/story-review.json`

  ```ralph-task
  {
    "id": "story.review",
    "after": ["story.draft"],
    "team": "studio/quality/narrative",
    "binding": {
      "mode": "pool",
      "alias": "",
      "provider": "",
      "model": "",
      "effort": "",
      "calibration": "",
      "repetitions": 0,
      "fixture": ""
    },
    "requires": ["local-agent"],
    "providers": ["claude", "codex"],
    "differentFrom": ["story.draft"],
    "inputs": [],
    "outputs": [
      {"path": "artifacts/story-review.json", "mode": "exclusive"}
    ]
  }
  ```
````

Replace the example hash with the file's real SHA-256. Imported hashes must
be exactly 64 lowercase hexadecimal characters.

All nine task fields and all eight binding fields are required, even when a
value is empty or an array is empty. Unknown fields, `null` values, duplicate
values, malformed IDs, unknown task references, cycles, and unsafe paths are
rejected before any plan row is created. Every v2 step must also declare
exactly one non-empty mechanical acceptance criterion: one `accept:` command,
one `accept-file:` path, or one of each. Duplicate or ambiguous declarations
are rejected.

| Field | Contract |
| --- | --- |
| `id` | Stable task identity: 1–128 lowercase letters, digits, `.`, `_`, or `-`. |
| `after` | Explicit dependency IDs. In v2 this DAG, not list type or heading order, determines readiness. |
| `team` | Hierarchical ownership path such as `studio/art/characters`. It is persisted with the task. |
| `binding` | Strict execution mode and its complete tuple; see below. |
| `requires` | Closed provider-capability list. Legacy intrinsic values are `local-agent` and `native-fanout`; measured capabilities are listed below. Unknown values fail import. |
| `providers` | Optional shipped-provider allowlist. Empty means the project's configured provider pool. |
| `differentFrom` | Tasks whose recorded independence domain may not run this task. Each reference must also be reachable through `after`, so provenance exists before scheduling. |
| `inputs` | Project-relative files plus exact SHA-256. Ralph hashes the bytes again immediately before dispatch. |
| `outputs` | Project-relative paths reserved with mode `exclusive`. |

Task metadata, dependency edges, team ownership, output declarations,
assigned provider, blocked reason, and completion evidence are durable SQLite
records. Import and graph materialization are one transaction.

### Binding modes and calibrated capabilities

`binding.mode` is one of:

| Mode | Exact contract |
| --- | --- |
| `pool` | `alias`, `provider`, `model`, `effort`, `calibration`, and `fixture` are empty and `repetitions` is `0`. Only legacy intrinsic requirements can be used. |
| `calibration` | Exact non-empty alias/provider/model/effort plus a fixture name and at least three repetitions. `calibration` is empty and `requires` must be empty because capabilities have not been measured yet. |
| `calibrated` | Exact non-empty alias/provider/model/effort plus an immutable `sha256:...` calibration content address. Repetitions are `0` and fixture is empty. |
| `await-calibration` | Exact non-empty alias/provider/model/effort, with no content address yet. Dispatch blocks until that unique alias is calibrated, then snapshots the immutable content address and automatically requeues the task. |

Exact shipped tuples fail closed. Claude models are `haiku`, `sonnet`, `opus`,
or a `claude-...` identifier and support `low`, `medium`, `high`, `xhigh`, or
`max`. Codex requires a `gpt-...` identifier and supports those efforts plus
`ultra`. OpenCode requires a `provider/model` identifier and the explicit
`default` effort. Aliases never substitute for or weaken the provider/model
check.

The measured capability vocabulary is:

```text
runtime.local-session
runtime.noninteractive
runtime.structured-output
runtime.usage-metered
runtime.resume
runtime.native-fanout
context.16k
context.128k
input.image
tools.repo-read
tools.repo-write
tools.shell
tools.browser-silent
quality.exact-citation
quality.schema-conformance
quality.graph-reasoning
quality.causal-narrative
quality.quantitative-systems
quality.code-build-test
quality.visual-critique
quality.pixel-composition
```

Built-in declarations and CLI help never grant these capabilities. A
`calibration` task performs the declared fixture as independent exact
invocations, persists the actual tuple, provider session, and assistant-output
hash for every repetition, and aggregates the evidence for a dependent
adjudication task. The adjudicator imports one complete calibration record with
`radioactive_ralph provider calibration import RECORD.json`. That record pins
the alias, provider, model, effort, binary path/version/SHA-256, invocation
configuration hash, inference/control/independence domains, capabilities, and
evidence. The record ID is content-addressed and its alias cannot be retargeted.

This makes a bootstrap graph self-progressing: fixture tasks run without
optimistic capabilities; adjudication mints the immutable record; downstream
`await-calibration` tasks for that alias are atomically requeued and bind the
record before their first provider turn.

V2 scheduling fails closed:

- no configured provider satisfying the allowlist, capability requirements,
  and `differentFrom` exclusions moves the task to `blocked_capability`;
- an `await-calibration` task without its alias record moves to
  `blocked_capability` and is automatically readmitted when that record is
  imported;
- a missing, changed, or symlink-escaped input moves it to `blocked_input`;
- an exclusive output that overlaps a running task's input or output anywhere
  in the same project remains unclaimed and is retried after the owner
  finishes; shared input/input readers may run together;
- output paths that overlap inside one plan are only legal when the DAG orders
  every reader and writer involved.

`RALPH_MAX_PARALLEL` is currently only a supervisor-wide safety ceiling; it is
not a per-work-class optimizer. Heterogeneous plans that need measured
global/class/provider/independence-domain budgets are blocked on the
[adaptive-concurrency contract](../design/plan-v2-adaptive-concurrency.md).
That follow-on keeps graph size unbounded and adds atomic, durable admission;
do not encode a fixed worker count into a plan as a substitute.

Input and output paths use portable forward-slash, project-relative spelling.
Absolute paths, `..` traversal, backslashes, and symlinks escaping the project
root are rejected.

## Approval gates

A step can be **held for human approval** before it runs. End the step's
text with the `[approval]` marker (case-insensitive):

```markdown
# Ship

1. build the release artifacts
2. Deploy to production [approval]
3. run smoke tests
```

The `[approval]` marker is stripped from the displayed step text and does
**not** change how the step reads. A gated step is materialized in the
`ready_pending_approval` state instead of `pending`, so the supervisor's
dispatch loop **skips it** — it is never claimed or run, and (in a
sequential group) the steps after it wait too, until an operator approves
it. Approve it from the desktop GUI (the **Approve** button on a gated
task) or the drive API — the terminal client is read-only and has no
approve action. Approval promotes the task to `ready`, and the next
dispatch tick claims and runs it normally.

Use it for the irreversible or high-blast-radius step in an otherwise
autonomous plan — a production deploy, a data migration, a destructive
cleanup — so the run pauses for a human check at exactly that point
without stopping everything before it. Bracketed text that isn't the
`[approval]` marker (e.g. a trailing `[WIP]`) is left untouched and does
not gate the step.

## Validation

`internal/plan.Validate` checks the document against the grammar (sibling
heading levels, ambiguous sections) and returns structured errors so a
malformed plan is caught before dispatch, not discovered mid-run.

## From a vague ask to a plan

Turning a free-form prompt into a plan document is the one place a human
ask needs interpretation. Rather than an interactive Q&A flow, a small
team of agents **juxtapose and challenge** each other's read of the draft
until it converges on a plan that covers the work end-to-end
(`internal/genesis`). Headless mode emits the final markdown; the TUI
renders it for review (scroll, or hand off to `$EDITOR`) before it's
accepted. You can also skip this and hand-write the plan directly — the
refined markdown document remains the source of truth. V2 task blocks add
execution contracts in place rather than compiling the plan into a second
file.

## How the orchestrator uses it

The orchestrator (`internal/orch`) computes what's ready from the legacy
plan AST or the materialized v2 dependency graph, dispatches ready steps
to agent workers with plan-scoped context, and **verifies** each
completion against the step's acceptance criteria (a command that must
exit 0, a file that must exist, or — absent either — the worker's
evidence output) before marking it done. A worker's own claim of
completion, or its process simply terminating, is never sufficient on its
own.
