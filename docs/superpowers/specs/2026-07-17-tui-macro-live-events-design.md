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

- **Always:** apply the lifecycle delta via the existing `applyEvent` (updates a
  task's status in `snap.tasks` when that task is loaded — i.e. live at meso; at
  macro `snap.tasks` isn't populated so `applyEvent` is a no-op there and the
  plan-progress bar reconciles on the next poll — see the note below), and
  prepend the event to a bounded `snap.planEvent` tail (the macro event pane) —
  this makes the macro pane a live tail.
- **At micro only:** additionally append to `snap.live` filtered to the selected
  task (today's behavior, unchanged — the per-task log pane).

> **Macro plan-progress:** live frames update the macro *event pane*
> immediately, but the macro plan-*progress* counters (`snap.progress`) come from
> `PlanProgress` and are not recomputed from a single event — they refresh on the
> 1s poll. Deriving a live progress delta from a `task.done`/`failed` frame is a
> possible follow-up; for now the event pane is the live signal at macro and
> progress lags by at most one poll.

Because the subscription is always live, drilling in/out no longer starts/stops
it; drillIn to micro just changes which pane the SAME stream also feeds, and
resets `snap.live`. This removes the `startAttach`-on-drillIn / cancel-on-drillOut
dance and the per-drill epoch churn — the epoch now changes only on a
reconnect, not on every navigation.

### Reconnect + lifecycle (as built)

`attachEndedMsg` drops the subscription's channels; the next `fetchedMsg` (the
1s poll always fires) calls `ensureAttach`, which restarts the subscription
(bumping the epoch). So a supervisor blip reconnects within ~1s rather than
leaving the feed dead — a working, low-latency reconnect.

**Known limitation (accepted for now):** the reconnect re-seeds its cursor from
the *current* `MaxEventID` (inside the data source's `Attach`), NOT the last-seen
id, so events that landed DURING the disconnect gap are not re-streamed. In
practice the 1s poll (which reads the DB directly) reconciles the macro
`planEvent` pane and the task state across the gap, so the user still sees those
events — just via the poll, not the live stream. Making the reconnect cursor-aware
(resume from the last-processed id) is a follow-up if the gap ever proves
user-visible; it needs the model to remember its last-seen id and thread it into
the resubscribe. The initial cursor is likewise seeded from `MaxEventID` inside
`liveDataSource.Attach` (already merged in #169/#173), consistent with how the
data source reads every other value straight from the store — no new
`DataSource` method and no IPC change.

### `snap.planEvent` as a live tail

`planEvent` becomes a rolling tail. Each live frame prepends (newest-first) via
`prependEvent`, id-deduped and capped. Critically, the poll must **merge** (not
wholesale-replace) its snapshot into the tail via `mergeEventTail`: a live event
whose DB commit lands after the poll's read began is absent from the poll result,
so a replace would permanently drop it (it's a one-shot stream frame, never
re-delivered). The merge unions live + poll deduped-by-id, newest-first, capped —
so the poll reconciles toward the DB WITHOUT regressing the live tail.

This also depends on `ListProjectEvents` (the poll's read) using the
plan-linkage `eventProjectScope`, so the poll and the live tail agree on which
events belong to the project (fixed in #178 — a bare `project_id` filter dropped
the plan-scoped lifecycle events the pane exists to show).

## Layering

- **tui** (`model.go`): start the session subscription on the first `fetchedMsg`
  (`ensureAttach`, idempotent) rather than on `drillIn`; route `liveFrameMsg` by
  level (always: `applyEvent` delta + `prependEvent` into the macro tail; micro
  adds the filtered per-task log). `drillOut` no longer cancels the stream; quit
  does. `mergeEventTail` reconciles the poll into the live tail without dropping
  a live event. No `DataSource` change: the cursor seed lives inside the existing
  `liveDataSource.Attach` (`MaxEventID` read, already merged), and the fake
  drives the whole flow through the existing `DataSource.Attach`.
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
