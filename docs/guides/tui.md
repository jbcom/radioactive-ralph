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
