---
title: Plan format
description: The markdown grammar plans are decomposed from, and how completion is verified.
---

# Plan format

Plans are plain markdown, decomposed **heuristically** over the parsed
document structure (`goldmark`, pure Go) — no LLM involved in
decomposition, no separate plan-definition language to learn.

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

## Task annotations (`ralph-task`)

A step can carry a fenced ```` ```ralph-task ```` block of JSON. This is
**optional**: every plan written before the grammar existed keeps working
unchanged, and an unannotated step is a perfectly valid graph node.

````markdown
# Build and verify

1. compile the binary

   ```ralph-task
   {"id": "build"}
   ```

2. run the integration suite

   ```ralph-task
   {"id": "integration", "after": ["build"]}
   ```

3. run the linters

   ```ralph-task
   {"id": "lint", "after": ["build"]}
   ```
````

Here `integration` and `lint` both wait on `build` and then run
**concurrently** — something heading order alone cannot express, because
document order would have forced them into a sequence.

### `after` — the one field that changes ordering

`after` has three distinct meanings, and the difference is load-bearing:

| Written as | Means |
|---|---|
| *(omitted)* | document order applies, exactly as before annotations existed |
| `"after": []` | an explicit **root** — no predecessors, ready immediately |
| `"after": ["a","b"]` | ready only once `a` and `b` are both done |

Omitting `after` is **not** the same as `"after": []`. Annotating a step
with a team or a binding must not silently reorder execution, so a block
with no `after` keeps whatever order the document already implied. Writing
`[]` is how you say "this genuinely has no predecessors."

A plan with no annotations imports as a chain of edges derived from
document order, so **a linear plan is the degenerate case of a DAG** rather
than a separate code path.

### Fields

Each field's **Status** column says what it actually does today, and the
distinction is load-bearing rather than bookkeeping:

- **enforced** — dispatch acts on it and refuses or defers when it cannot be
  satisfied. `id`, `after`, `providers`, `differentFrom`, and
  `binding.provider` are here.
- **persisted** — recorded on the task, consumed by nothing yet (`team`).
- **parsed** — accepted by the grammar and not yet acted on.

A field that only parses is NOT a guarantee, and this table is the place to
check before relying on one. Fields are recorded even before they are enforced
so the consuming feature can be added without a plan-format change — but that
also means a plan can declare something the runtime ignores, which is exactly
what the Status column exists to make visible.

| Field | Status | Meaning |
|---|---|---|
| `id` | **enforced** | stable task id, **required whenever a `ralph-task` block is present** — the parser rejects a block whose `id` is missing or empty. A step with NO block gets the positional id (`0.1`). Must be unique within the plan. |
| `after` | **enforced** | dependency edges (see above). |
| `team` | **persisted** | slash-delimited path, stored as the task's `team_path`. Nothing consumes it yet: `TeamRollups` exists but has no caller, and the operator snapshot does not expose a team field, so it does not currently affect any view. |
| `binding` | **enforced** (`provider`) | pins provider identity: `mode`, `alias`, `provider`, `model`, `effort`, `calibration`, `repetitions`, `fixture`. `provider` is matched against the binding's TYPE at dispatch — an alias merely *named* after a provider does not satisfy it — and a partition is never coalesced across differing pins. The other sub-fields are parsed and not yet consumed. |
| `requires` | parsed | capability keys the bound provider must satisfy. |
| `providers` | **enforced** | restricts the task to a subset of configured providers, matched against a binding's alias OR type. Dispatch rotates the pool to find a permitted binding and refuses if none exists; a native fan-out group never coalesces a restricted task, since one turn cannot honour a per-task restriction. |
| `differentFrom` | **enforced** | task ids that must not share this task's independence domain. References are checked at import (unknown, self-referencing, whitespace-padded, and CYCLIC references are all rejected), and dispatch rotates to a binding whose calibrated domain differs from every named peer. A peer with an unknown domain DEFERS rather than passing — a constraint that passes on absence of evidence enforces nothing. See below. |
| `inputs` | parsed | files the task reads: `{"path": ..., "sha256": ...}`. |
| `outputs` | parsed | files the task writes: `{"path": ..., "mode": "exclusive"}`. |

### The grammar fails closed

A malformed block is refused at import rather than silently ignored,
because a dropped annotation changes what runs:

- an **unknown field** is an error, not a no-op — a typo'd `dependsOn`
  would otherwise import with no edges and run in the wrong order;
- `"after": null` is refused, because null and omitted are
  indistinguishable after decoding and they mean different things;
- a **duplicate key** is refused. `encoding/json` keeps the *last* value,
  so `{"after":["prepare"],"after":[]}` would decode as an unconditioned
  root and dispatch **before** `prepare` — the exact ordering guarantee the
  grammar exists to provide;
- **two blocks on one step** is refused rather than picking one;
- an `after` naming a task that is not in the plan is refused;
- a **cycle** is refused (the store's `AddDep` checks it).

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

## Capability requirements

A step can declare the provider capabilities it needs, in a `ralph-task`
fence indented inside the step's list item:

```markdown
# Wide refactor

- rename the symbol across every call site

   ```ralph-task
   {"id": "rename-all", "requires": ["native_fanout"]}
   ```
```

Dispatch resolves the binding the step would run on and checks it
**before** allocating a worker, a session, or a dispatch slot. A step
whose requirements the binding cannot meet is moved to
`blocked_capability` with the missing keys recorded as its blocked
reason, rather than being run against a provider that cannot do the work.

The vocabulary is closed — a key outside it is a typo, and a typo cannot
be satisfied by any binding, so it blocks the step rather than passing
silently:

| Key | The binding must… |
|-----|-------------------|
| `native_fanout` | run a whole ready group in one turn (`native_fanout = true`) |
| `resume` | continue a prior session (`supports_resume = true`) |
| `append_system_prompt` | append to the system prompt rather than replace it (`use_append_system_prompt = true`) |

Every key maps to something a binding declares in its config, so the two
cannot drift apart. Steps that declare no `requires` are unaffected.

Unblocking is a plan or configuration change — fix the key, or bind the
project to a provider that has the capability — not something the
supervisor retries its way out of. That is the point: a task that can
never succeed on this binding says so immediately instead of stalling
with no explanation.

## Restricting which provider runs a task

A step can name the providers allowed to run it:

```markdown
# Cross-check

- generate the migration

   ```ralph-task
   {"id": "generate-migration"}
   ```

- review the generated migration

   ```ralph-task
   {"id": "review-migration", "providers": ["codex"]}
   ```
```

A name matches either the binding **alias** or the provider **type**, because
both are useful restrictions: several aliases can share one type (a
round-robin pool of `claude` bindings), so "any claude" and "this specific
pool member" mean different things.

A task whose resolved provider is outside its list is **not dispatched**, and
the refusal is recorded as a `worker.admission_refused` event naming the
allowed providers. Where the project is bound to a *pool*, dispatch rotates
through it first: a `[claude, codex]` pool can still run a codex-only task, so
the restriction is about the work rather than about which member the rotation
happened to reach.

The task stays `pending` rather than becoming `blocked_capability`, and the
difference from `requires` is deliberate. `providers` names operator
**configuration** — binding the project to an allowed provider fixes it, so a
durable block would leave an operator clearing a state their config edit had
already resolved. `requires` is different: no configuration change makes a
provider gain a capability it does not have, so that one does block.

Unlike `requires`, there is no closed vocabulary here: provider names are
operator-chosen configuration, so an unrecognized name is indistinguishable
from a provider this project simply is not bound to right now. Either way the
task blocks and names the list, which is the actionable report.

## Independence constraints

A step can declare that it must not run on the same provider as another task —
for work whose value depends on *who* did it, like a review of generated code:

```markdown
# Cross-check

- produce the migration

   ```ralph-task
   {"id": "produce"}
   ```

- review it

   ```ralph-task
   {"id": "review", "differentFrom": ["produce"]}
   ```
```

**Import validates the references and dispatch enforces the constraint.** Both
halves matter, and the split is worth being precise about, because a
half-implemented guarantee is easy to mistake for a whole one.

What is checked now:

- A reference to a task that does not exist is **rejected**. It could never be
  satisfied or violated, so it would sit in the plan looking like an
  independence guarantee while enforcing nothing — worse than an unenforced
  field, because the plan reads as protected.
- A self-reference is **rejected**. A task cannot run on a provider different
  from its own, so dispatch could only ever block it forever; failing at import
  turns a permanent stall into an authoring error.
- An **empty** entry is rejected, for the same reason as an unknown one: it names
  no task, so it can never be enforced. Skipping it silently would also give you
  no signal that a list entry did nothing.

What dispatch does with it: before a step runs, its peers' RECORDED domains --
what they actually ran on, not what the plan hoped -- are compared against the
domain of the binding this turn would use. A clash ROTATES through the provider
pool rather than blocking, because a pool is a rotation and the next member may
well differ; refusing on the head's identity would make the constraint a coin
flip on cursor position. It is deliberately not a fail-closed `blocked_*` state
either: no operator action is needed and the next dispatch may satisfy it.

An UNCALIBRATED peer does not satisfy the constraint. An empty domain means the
peer has not run yet, or ran on a binding nothing has calibrated -- either way
the domains are not *known* to differ, and a constraint that passes on absence
of evidence enforces nothing while reading as protection. The step defers until
the peer has run on a calibrated binding. Record one with
`radioactive_ralph calibration record`.

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
refined document *is* the plan; there's no separate machine format it
gets compiled into.

## How the orchestrator uses it

The orchestrator (`internal/orch`) computes what's ready from the plan's
AST plus the database's done-state for each step, dispatches ready steps
to agent workers with plan-scoped context, and **verifies** each
completion against the step's acceptance criteria (a command that must
exit 0, a file that must exist, or — absent either — the worker's
evidence output) before marking it done. A worker's own claim of
completion, or its process simply terminating, is never sufficient on its
own.
