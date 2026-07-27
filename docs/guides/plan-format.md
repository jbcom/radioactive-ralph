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

- review the generated migration

   ```ralph-task
   {"id": "review-migration", "providers": ["codex"]}
   ```
```

A name matches either the binding **alias** or the provider **type**, because
both are useful restrictions: several aliases can share one type (a
round-robin pool of `claude` bindings), so "any claude" and "this specific
pool member" mean different things.

Like a capability requirement, a task bound to a provider outside its list
moves to `blocked_capability` with the allowed names recorded as its reason,
rather than running somewhere the plan says it must not. Both are the same
kind of refusal — *this provider may not do this work* — so they share one
status instead of splitting into two an operator has to learn separately.

Unlike `requires`, there is no closed vocabulary here: provider names are
operator-chosen configuration, so an unrecognized name is indistinguishable
from a provider this project simply is not bound to right now. Either way the
task blocks and names the list, which is the actionable report.

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
