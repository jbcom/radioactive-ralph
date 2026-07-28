---
title: TUI
description: The read-only macro/meso/micro client view.
---

# TUI

Running `radioactive_ralph` in a terminal renders a read-only Bubble Tea
TUI over the supervisor's live state. Piped or non-interactive invocations
(CI, `go test`, a redirected stdout) print a single status line instead —
the TUI never launches without a real terminal to drive it.

The TUI is strictly read-only: it calls only read methods on the
supervisor's IPC client and the shared store (`internal/tui.DataSource`).
It never dispatches work and never mutates durable state. All mutation —
what runs next, when a step is verified done — is the orchestrator's job,
not something a human triggers from the client.

## Drill-down levels

| Level | Shows |
|-------|-------|
| Macro | The project's plan and its overall group hierarchy |
| Meso | One plan group drilled in — its steps, or (for a parallel step-group) the squad of workers running it |
| Micro | One worker — its live pane or log tail |

Each level is a view over the same live snapshot the supervisor holds;
there is no separate client-side state to get out of sync.

### Task rows

A meso task row reads `<id> <status> <description>`, followed by two
markers that appear only once they are known:

- `worker=<id>` — the Ralph worker currently holding the task's claim.
- `via=<alias>` — the provider that actually executed the task, by its
  configured alias (falling back to the provider type when no alias was
  set).

`via=` is per task rather than per worker because it **outlives the
worker**. `worker=` is a live claim: the reaper deletes worker rows once
they stop heartbeating, and a finished task's claim is released. Provenance
lives in the task's own metadata, so a `done`, `failed`, or reaped task
still reports what ran it long after no worker row exists to ask.

It also survives reassignment. A retry or reclaim overwrites the
assignment, so `via=` names the provider on the task's *current* attempt
rather than whatever ran an earlier one.

(Within a single native fan-out group these values agree by construction —
one turn, one binding, recorded onto every task in the group. The per-task
projection is about durability across time, not disagreement within a
group.)

A task that has not been dispatched shows no `via=` at all — an unexecuted
task must not read as though some provider owned it.

- `p1`, `p2`, … — the **ready partition** the task belongs to. Rows sharing
  a marker are the ones native fan-out may hand to a single provider turn.

Only partitions holding more than one task are marked. A partition of one
is the ordinary case, so labelling every row would bury the fan-out groups
the marker exists to reveal. The numbers are per view, not global ids: the
underlying ordinal is a hash, and the only question it answers is "same
partition or not?" — deliberately not "pinned to what?", since a
partition's real identity embeds the plan author's own binding text, which
this surface withholds.

## What it reads

- `Status` — supervisor status snapshot (worker counts, task counts,
  recent heartbeat)
- `ListPlans` / `PlanProgress` / `ListTasks` — the current project's plan
  and task state
- `ListProjectEvents` / `ListTaskEvents` — recent event history
- `Attach` — the live event stream, so the view updates as work
  progresses

## Relationship to the CLI

There is no separate `tui` subcommand and no separate cockpit runtime.
Plain `radioactive_ralph` *is* the TUI (when run interactively); it is a
view, not a second control surface.
