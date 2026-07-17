# Attach live consumers — apply deltas in the TUI/GUI

**Status:** accepted (2026-07-17). Depends on #169 (the attach event producer).

## Problem

#169 makes the supervisor STREAM structured events over Attach. But the
clients don't yet USE them as structured data:

- **TUI** (`internal/tui/model.go`): the micro view subscribes (`startAttach` →
  `liveFrameMsg`) and appends every frame to `snap.live` as opaque log text
  (`renderFrame`). Two gaps: (1) it appends **every project frame** regardless
  of which task is selected — the codex P2 deferred from #169 — so drilling into
  one task shows unrelated tasks' activity; (2) the macro/meso views
  (plans/tasks/status) still only update on the **poll tick**, never from a live
  event, so a `task.done` doesn't visibly land until the next poll.
- **GUI** (`internal/gui`): `runAttach` calls a full `refreshNow()` on every
  frame — correct-but-blunt: it re-reads everything on any event rather than
  applying the one delta.

## Goal

Make the live view actually live and correct:

1. **Filter the micro-view tail to the selected task** (codex P2): only append a
   frame whose `task_id` matches `selectedTask` (or is task-agnostic and
   relevant). Decode frames as `ipc.AttachEvent` instead of re-probing raw JSON.
2. **Apply lifecycle deltas to the macro/meso snapshot** so a `task.done`,
   `task.failed`, `task.claimed`, `worker.*`, `plan.*` event updates the visible
   plan/task state immediately, not on the next poll.
3. **Keep the poll as a reconcile safety net** — the push feed is best-effort
   (a dropped/duplicated frame, a reconnect gap); the periodic poll remains the
   source of truth that corrects any drift. Deltas make the view *feel* live;
   the poll guarantees it's *eventually correct*.

Non-goals: no new IPC surface (the wire shape is #169's `AttachEvent`); no
removal of the poll; no per-kind exhaustive handling — unrecognized kinds fall
through to "append to the relevant log + let the next poll reconcile."

## Approach

### Decode once, at the boundary

Replace `renderFrame(raw)`'s ad-hoc probe with a single
`json.Unmarshal(raw, &ipc.AttachEvent{})` at the `liveFrameMsg` handler. Every
downstream decision (filter, delta, render) reads typed fields. A decode error
is logged and the frame dropped (it can't be acted on).

### TUI micro-view filter (codex P2)

In the `liveFrameMsg` case, before appending to `snap.live`:

```
ev := decode(msg.raw)
if m.lvl == micro && ev.TaskID != "" && ev.TaskID != m.selectedTask.ID {
    // a different task's event — don't pollute this task's tail
    return m, rearm()
}
m.snap.live = append(m.snap.live, liveLogLine{at: ev.OccurredAt, text: render(ev)})
```

A frame with no `task_id` (plan/service-level) is still shown at micro (it may
be relevant context), matching today's behavior for those.

### Macro/meso delta application

A small `applyEvent(snap, ev) snapshot` maps a lifecycle kind to a targeted
snapshot mutation:

- `task.done` / `task.failed` / `task.failed_terminal` / `task.claimed` /
  `task.released` → find the task by `ev.TaskID` in `snap.tasks` (meso) and
  update its status field; bump the matching plan's progress counters if cheap,
  else mark the plan dirty for the next poll.
- `worker.verified_done` / `worker.completed` → same as task.done.
- `plan.imported` / plan status kinds → mark plans dirty (a new plan needs a
  full ListPlans; a delta can't synthesize it).
- unknown kind → no snapshot mutation; the log line + next poll cover it.

"Mark dirty" = set a `pendingReconcile` flag the next `refreshMsg` honors by
doing a full fetch (it already fetches every tick, so "dirty" just means "don't
trust the delta for this entity until the next fetch confirms"). Because the
poll runs every tick regardless, the delta is a *fast path*, never the only
path — this is what makes correctness robust to any missed/dup event.

### GUI: same shape, lighter touch

Replace `runAttach`'s unconditional `refreshNow()` with: decode the frame, and
only call `refreshNow()` for kinds that change visible aggregate state
(task/plan/worker lifecycle), skipping pure log/heartbeat kinds (`tick`,
`task.progress` unless the progress view is open). This cuts the redundant
full-refresh storm the #169 review flagged while keeping the GUI correct. (The
GUI already re-reads from the store, so it needs no per-field delta logic — just
a smarter trigger.)

## Layering

- **tui** (`model.go`): decode frames as `ipc.AttachEvent`; add the micro-view
  task filter and `applyEvent` + `pendingReconcile`. `renderFrame` becomes
  `renderEvent(ipc.AttachEvent) string`.
- **gui** (`app.go`): gate `refreshNow()` on the decoded event kind.
- No store/ipc/supervisor change.

## Testing

- tui: a `liveFrameMsg` for a non-selected task at micro is NOT appended; one
  for the selected task IS. A `task.done` event flips the matching task's status
  in `snap.tasks` without a poll. An unknown kind is a no-op on the snapshot but
  still logs. Decode error is dropped without crashing.
- gui: `runAttach` calls refreshNow for a lifecycle kind and skips it for a
  `tick`/heartbeat kind (assert via the fake controller's refresh counter).
- The poll-reconcile net: after a delta marks a plan dirty, the next fetch does
  a full read (existing behavior; assert the flag is honored/cleared).

## Why this is the right follow-up

#169 built the pipe and proved it end-to-end; this makes the pixels move. It is
the smallest change that (a) fixes the deferred codex P2 correctness bug, (b)
turns "poll-only" into "push-live" for the user, and (c) removes the GUI's
full-refresh-per-frame waste — all without touching the producer or the wire.
