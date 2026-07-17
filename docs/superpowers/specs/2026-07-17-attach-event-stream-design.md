# Attach event stream — design

**Status:** accepted (2026-07-17)

## Problem

The IPC drive+observe API has two halves. The *drive* half works: clients
enqueue, approve, pause, kill, import. The *observe* half is a stub. The
supervisor's `HandleAttach` (`internal/supervisor/supervisor.go`) blocks on
`<-ctx.Done()` and **emits nothing** — an Attach client connects and receives
silence until it disconnects. The IPC transport for it is fully built and
hardened (server streams `emit` frames until ctx cancel; #160 added write
deadlines and a request cap; #165 added the read-side disconnect watcher that
cancels the handler ctx on client EOF), but there is no producer.

Meanwhile the supervisor already writes a rich, append-only audit log. Every
load-bearing state transition emits an `events` row via `store.Emit` or an
inline `INSERT INTO events` inside the same transaction as the transition:
`task.claimed`, `task.done`, `task.failed`, `task.failed_terminal`,
`task.released`, `task.blocked`, `task.context_requested`, `task.progress`,
`worker.verified_done`, `worker.verification_failed`, `worker.result`,
`worker.spend`, `worker.admission_refused`, `worker.dispatch_error`,
`worker.dispatch_panic`, `plan.imported`, `project.created`, `service.started`,
`tick`, and more. The `events` table has a monotonic `id INTEGER PRIMARY KEY
AUTOINCREMENT`.

But `store.ListProjectEvents` — the only read path — has **no callers**. The
data is written and never surfaced. TUI and GUI render by *polling* the store
(`Status` + `ListPlans` + `ListTasks`) on a timer; they never see events, and
between polls the live view is stale.

## Goal

Turn the observe half on: stream the already-persisted event rows to Attach
clients as they are written, so a read-only TUI/GUI live view goes from
poll-only to push-live. Ship the smallest correct thing that makes
`HandleAttach` a real producer, reusing the existing events table as the single
source of truth — **no new event type, no new write path, no dual-write.**

Non-goals (YAGNI): no server-side filtering DSL, no per-client subscription
config, no event replay UI, no retention/compaction changes, no new event
kinds. Those can follow if a need surfaces.

## Approach — tail the events table

The events table is append-only with a monotonic `id`. "Stream new events" is a
tail: repeatedly select rows with `id > lastSeen`, emit each, advance
`lastSeen`. Chosen over the two alternatives:

- **In-process pub/sub bus** (emitters also push to subscriber channels): needs
  a second delivery path parallel to the DB write, with its own ordering,
  backpressure, and drop semantics, and every current + future emit site must
  remember to publish. The table already *is* the ordered, durable log; a tail
  reuses it with zero emitter changes. Rejected as premature.
- **SQLite `sqlite3_update_hook`**: a C-level row-change callback would avoid
  polling, but it is per-connection (our pool has up to 4 conns — #129), fires
  on the writing connection only, and couples us to driver internals. Rejected
  as fragile for the benefit.

The tail polls the DB on a short interval. This keeps a single ordered delivery
path (the DB), survives supervisor restarts (a reconnecting client resumes from
its last id), and needs no change to any of the ~20 emit sites.

### Cursor semantics — connect-time snapshot, then live

On attach the client passes an optional `AfterID`. The supervisor:

1. If `AfterID == 0` (fresh attach), seeds the cursor to the **current max
   event id** so the client starts with the live tail, not the entire history.
   A separate one-shot backlog read (existing `ListProjectEvents`, exposed
   over IPC as a normal request/reply) hydrates the initial view; Attach is
   purely the live delta. This keeps the first frame small and bounded.
2. If `AfterID > 0` (reconnect), resumes from exactly that id, so a client that
   dropped and reconnected sees every event it missed with no gap and no
   duplicate. This is what makes #165's reconnect loop (GUI `runAttach`)
   correct rather than lossy.

The tail loop each tick: `SELECT ... WHERE id > ? ORDER BY id ASC LIMIT N`,
emit each row as one frame, set cursor to the last id returned. `LIMIT N`
(e.g. 256) bounds a single tick's work if a burst landed; the next tick drains
the rest immediately.

### Scope — project

Attach is already established per connected client, and a client is scoped to
one project (the supervisor discovers the project from the connection's init).
The tail filters `WHERE project_id = ?` so a client only sees its own project's
events — matching `ListProjectEvents`' existing scoping. Events with a NULL
project_id (a few service-level kinds like `tick`) are **not** streamed to
project clients; they are service-internal and add noise.

## Event frame schema

Each streamed frame is the JSON encoding of a stable, public event shape —
NOT the raw `store.Event` (which exposes DB column quirks). A new
`ipc.AttachEvent`:

```go
// AttachEvent is one event streamed over an Attach connection. It is the
// public, versioned shape of an events-table row; payload is the kind's
// already-JSON payload passed through verbatim.
type AttachEvent struct {
    ID         int64           `json:"id"`
    Kind       string          `json:"kind"`             // e.g. "task.done"
    Stream     string          `json:"stream,omitempty"` // "service"|"worker"|...
    PlanID     string          `json:"plan_id,omitempty"`
    TaskID     string          `json:"task_id,omitempty"`
    Actor      string          `json:"actor,omitempty"`
    Payload    json.RawMessage `json:"payload,omitempty"` // kind-specific, pass-through
    OccurredAt time.Time       `json:"occurred_at"`
}
```

`ID` lets a client persist its resume cursor. `Payload` is the row's
`payload_json` passed through as `json.RawMessage` — consumers that care about a
kind decode it; the stream itself stays kind-agnostic, so adding a new event
kind never requires a transport change. The frame carries `AttachEvent`
directly (the IPC layer already frames each `emit` call as one length-prefixed
JSON message).

## Layering

- **store** (`internal/store/events.go`): add
  `EventsAfter(ctx, projectID string, afterID int64, limit int) ([]Event,
  error)` — `WHERE project_id = ? AND id > ? ORDER BY id ASC LIMIT ?` — and
  `MaxEventID(ctx, projectID string) (int64, error)` for the connect-time
  snapshot seed. Leaf, no new deps. Reuses `scanEvent`.
- **supervisor** (`internal/supervisor`): implement `HandleAttach` as the tail
  loop — seed cursor (MaxEventID or the client's AfterID), then on a ticker call
  `EventsAfter`, map each `store.Event` → `ipc.AttachEvent`, `emit` it, advance
  the cursor, until `ctx.Done()`. Interval is a small const (e.g. 250ms);
  `emit` returning an error (client gone / write deadline) ends the loop,
  matching the existing contract.
- **ipc** (`internal/ipc`): add `AttachEvent`; extend `AttachArgs`/the attach
  request with `AfterID int64`. `Client.Attach` already streams
  `json.RawMessage` frames; add a typed `Client.AttachEvents(ctx, afterID,
  func(AttachEvent) error)` convenience that unmarshals each frame, leaving the
  raw `Attach` intact.
- **consumers** (later tasks, incremental): TUI/GUI live view subscribes via
  `AttachEvents` and applies deltas, falling back to the existing poll as a
  safety net. Kept out of the first PR to keep it reviewable; the first PR makes
  the producer real and testable end-to-end via a fake client.

## Error handling

- A DB error inside the tail is logged and the tick is skipped (transient); the
  loop continues so a momentary lock (pool contention) doesn't kill a live
  stream. The cursor is only advanced past rows actually emitted.
- `emit` error (slow/vanished client) ends `HandleAttach` cleanly — this is the
  existing contract; #165's watcher and the per-frame write deadline already
  guarantee the loop can't wedge.
- A reconnecting client with a stale `AfterID` older than any surviving row
  simply gets everything from `afterID` forward; events are append-only and not
  pruned by this feature, so there is no "cursor too old" case to handle now.

## Testing

- store: `EventsAfter` returns only rows with `id > afterID`, ascending, capped
  at limit, scoped to project; `MaxEventID` on an empty project returns 0.
- supervisor: with a fake `emit`, attaching to a project with pre-existing
  events + `AfterID=0` emits nothing until a *new* event lands, then emits it;
  with `AfterID=k` emits exactly the rows after k in order; the loop exits when
  ctx is cancelled and when `emit` returns an error.
- ipc: `AttachEvents` round-trips an `AttachEvent` through the frame codec;
  malformed frame surfaces a decode error to the callback path.
- `-race` on the supervisor tail (ticker goroutine vs. ctx cancel).

## Why this is the right first feature after the audit sweep

It is the highest-leverage product gap: the observe half of the headline
drive+observe API is inert, the transport for it is already built and hardened,
and the data already exists and is thrown away. This wires an existing
producer to an existing consumer channel with one small store query and one
supervisor loop — high value, contained blast radius, no schema change.
