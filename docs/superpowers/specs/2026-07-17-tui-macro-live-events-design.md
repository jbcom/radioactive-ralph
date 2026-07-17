# TUI live event tail at the macro/meso levels

**Status:** accepted (2026-07-17). Depends on the merged attach event stream
(#169) and its TUI consumer (#173).

## Problem

The TUI's live Attach subscription only runs at the **micro** level: `startAttach`
fires on drill-into-micro (`model.go` drillIn → levelMicro) and `drillOut` from
micro cancels it. So the **macro** plan-overview and **meso** task-list views —
the two levels a user watching an active run spends most time in — refresh their
event/state panes only on the 1s poll tick, never live. A `task.done` on the
macro `planEvent` pane appears up to a poll interval late, and the macro event
list is a poll snapshot, not a live tail.

Meanwhile #173 already built the machinery to apply an `ipc.AttachEvent` as a
delta (`applyEvent`/`taskDeltaStatus`) and to render frames (`renderEvent`) —
but only the micro subscription feeds it.

## Goal

Make the live event feed run for the **whole TUI session**, not just at micro,
so the macro/meso views update from events as they land (with the poll as the
reconcile net, exactly as at micro). A `task.claimed`/`done`/`failed` visibly
lands on the macro plan-progress and the macro event pane immediately; the macro
event list becomes a live tail, not a 1s poll.

Non-goals: no change to the wire/store layer; no per-level *resubscribe* (one
subscription serves all levels); no removal of the poll.

## Approach — one session-long subscription, dispatched by level

Move the subscription from "start on micro drill-in / stop on drill-out" to
"start once at `Init`, run until the session ends". The single subscription is
project-scoped (`AttachArgs{ProjectID, AfterID: MaxEventID-at-start}`) — the
whole project's events, not one task's. `liveFrameMsg` then routes each decoded
`ipc.AttachEvent` by the CURRENT level:

- **Always:** apply the lifecycle delta to `snap.tasks`/plan progress via the
  existing `applyEvent` (so meso/macro state is live regardless of which level is
  showing), and prepend the event to a bounded `snap.planEvent` tail (the macro
  event pane) — this makes the macro pane a live tail.
- **At micro only:** additionally append to `snap.live` filtered to the selected
  task (today's behavior, unchanged — the per-task log pane).

Because the subscription is always live, drilling in/out no longer starts/stops
it; drillIn to micro just changes which pane the SAME stream also feeds, and
resets `snap.live`. This removes the `startAttach`-on-drillIn / cancel-on-drillOut
dance and the per-drill epoch churn — the epoch now changes only on a
reconnect, not on every navigation.

### Reconnect + lifecycle

The session subscription reconnects on stream end (supervisor restart / socket
drop) the same way the micro one does today, re-seeding its cursor from the
last-seen id so no macro event is missed across a blip. `Init` seeds the initial
cursor via the data source (a new `DataSource.MaxEventID`-style read, or the
first backlog gather's max id — reuse the client-owned-cursor rule from the CLI
so the initial macro `planEvent` poll and the live tail don't double-count).

### `snap.planEvent` as a live tail

Today `planEvent` is replaced wholesale each poll (newest-first, capped at 10).
With the live feed it becomes a rolling tail: the poll still refills it
(reconcile), and each live frame prepends (newest-first) and truncates to the
cap. A live frame whose id is already in the poll snapshot is de-duped by id so a
poll landing right after a live prepend doesn't show the event twice.

## Layering

- **tui** (`model.go`): move `startAttach` from `drillIn` to `Init`/first tick;
  make the subscription session-long; route `liveFrameMsg` by level (macro/meso
  delta + planEvent tail; micro adds the filtered per-task log). `drillOut` no
  longer cancels the stream. Add id-dedup to the planEvent prepend.
- **datasource** (`datasource.go`): the subscription needs the project cursor at
  start; add the max-id read to the `DataSource` interface (live impl over the
  store, fake in tests) — or derive it from the first macro gather.
- No store/ipc/supervisor change.

## Testing

- A macro-level `liveFrameMsg` for a `task.done` updates `snap.tasks` status AND
  prepends to `snap.planEvent` (live tail), without a poll.
- The same event arriving live and then in a subsequent poll appears once
  (id-dedup).
- Drilling micro→meso→macro does NOT cancel the subscription (the stream keeps
  feeding); drilling into micro still filters `snap.live` to the selected task.
- Reconnect re-seeds the cursor; no macro event lost across a simulated stream
  end.

## Why this is the right next feature

It closes the biggest remaining gap in the push-live work: the levels a user
actually watches an active run from (macro/meso) were still poll-only. It reuses
the #173 delta machinery, simplifies the subscription lifecycle (one stream, not
per-drill start/stop), and needs no core change.
