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

### Cursor semantics — client-owned cursor, no server-side "seed to max"

The cursor is **entirely the client's**. `HandleAttach` streams every event
with `id > AfterID`, full stop — the server never substitutes a "current max"
for a zero cursor. `AfterID == 0` therefore means "from the beginning," not
"from now." This closes a lost-event race that a server-side seed-to-max would
open:

> A naive fresh-attach — client reads the backlog via a one-shot request, then
> attaches with `AfterID=0`, and the server seeds the cursor to `MAX(id)` *at
> attach time* — can permanently drop an event inserted between the backlog read
> and the attach: it is past the backlog snapshot yet at or below the seeded max,
> so it appears in neither.

The client instead owns a single monotonic cursor across both reads:

1. **Backlog hydrate** (optional, for a populated initial view): the client
   issues a normal request/reply that returns the recent backlog **and the
   max event id at read time** (`ListProjectEvents` already reads newest-first;
   the highest id in that page, or a dedicated `MaxEventID`, is the cursor). If
   the client wants no backlog, it uses `MaxEventID` alone.
2. **Attach** with `AfterID` set to exactly that cursor. Any event inserted
   after the backlog snapshot has `id > cursor`, so the live stream delivers it —
   no gap. Any event already in the backlog has `id <= cursor`, so it is not
   re-sent — no duplicate.
3. **Reconnect** re-uses the same rule: the client passes the highest id it has
   processed, and resumes with no gap and no duplicate. This is what makes
   #165's reconnect loop (GUI `runAttach`) correct rather than lossy.

`MaxEventID` and `ListProjectEvents` share the same connection/serialization as
the subsequent attach, and the client reads-then-attaches, so the cursor it
carries into the attach is a real observed id; there is no window where the
server invents one.

The tail loop each tick: `SELECT ... WHERE id > ? ORDER BY id ASC LIMIT N`,
emit each row as one frame, set cursor to the last id returned. `LIMIT N`
(e.g. 256) bounds a single tick's work if a burst landed; the next tick drains
the rest immediately.

### Scope — project, including plan-linked events

An Attach connection is not implicitly project-scoped: the IPC connection has
no init handshake, and the supervisor is deliberately project-agnostic (it
serves all of a machine's projects on one socket). So the **client passes the
project id in `AttachArgs`** (as the drive commands already pass a project id in
their args), and `HandleAttach` scopes the tail to it.

Scoping cannot be a bare `WHERE project_id = ?`, because **the headline
lifecycle events do not populate `project_id`.** The transactional inserts in
`internal/store/tasks.go` (`task.claimed`, `task.done`/`worker.completed`,
`task.failed`, `task.failed_terminal`, `task.released`, `task.blocked`,
`task.progress`, …) set only `plan_id`/`task_id`. A `project_id`-only filter
would silently drop exactly the events a live view exists to show. The tail
therefore scopes by project **through plan linkage** too:

```sql
WHERE id > ?
  AND ( project_id = ?
        OR plan_id IN (SELECT id FROM plans WHERE project_id = ?) )
```

`plans.project_id` is `NOT NULL REFERENCES projects(id)`, so a plan-scoped event
resolves to exactly one project. Events with neither a `project_id` nor a
`plan_id` (a few service-internal kinds like `tick`) are not delivered to any
project client — they are noise, not observability.

## Event frame schema

Each streamed frame is the JSON encoding of a stable, public event shape —
NOT the raw `store.Event` (which exposes DB column quirks). A new
`ipc.AttachEvent`:

```go
// AttachArgs is the client's CmdAttach payload. ProjectID scopes the stream
// (the connection carries no implicit project). AfterID is the client-owned
// resume cursor: the stream carries every event with id > AfterID (0 means
// from the beginning — the client, not the server, chooses the live-tail
// cursor by first reading MaxEventID).
type AttachArgs struct {
    ProjectID string `json:"project_id"`
    AfterID   int64  `json:"after_id,omitempty"`
}

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

- **store** (`internal/store/events.go`): add `EventsAfter(ctx, projectID
  string, afterID int64, limit int) ([]Event, error)` — the tail query, scoped
  to a project **including plan-linked events**:
  `WHERE id > ? AND (project_id = ? OR plan_id IN (SELECT id FROM plans WHERE
  project_id = ?)) ORDER BY id ASC LIMIT ?` — and `MaxEventID(ctx, projectID
  string) (int64, error)` with the **same** scoping, so the client's initial
  cursor and the tail agree on which rows belong to the project. Leaf, no new
  deps. Reuses `scanEvent`.
- **supervisor** (`internal/supervisor`): implement `HandleAttach(ctx, args,
  emit)` as the tail loop — cursor starts at `args.AfterID` (no server-side
  seed-to-max), then on a ticker call `EventsAfter(ctx, args.ProjectID, cursor,
  N)`, map each `store.Event` → `ipc.AttachEvent`, `emit` it, advance the cursor
  to the last emitted id, until `ctx.Done()`. Reject an empty `args.ProjectID`.
  Interval is a small const (e.g. 250ms); `emit` returning an error (client gone
  / write deadline) ends the loop, matching the existing contract.
- **ipc** (`internal/ipc`): add `AttachArgs{ProjectID, AfterID}` and
  `AttachEvent`; change the `Handler.HandleAttach` interface to
  `(ctx, args AttachArgs, emit)` and parse `AttachArgs` from the request in the
  server's CmdAttach path (a malformed args blob is an `invalid_args` response).
  `Client.Attach` already streams `json.RawMessage` frames; add a typed
  `Client.AttachEvents(ctx, args AttachArgs, func(AttachEvent) error)`
  convenience that sends the args and unmarshals each frame, leaving the raw
  `Attach` intact. Expose `MaxEventID`/backlog over IPC (a small
  request/reply) so a client can obtain its initial cursor.
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
  at limit, scoped to project — **including a plan-scoped event whose row has no
  `project_id`, only a `plan_id` belonging to the project** (the P1 that a bare
  `project_id` filter would drop), and excluding another project's events and
  unscoped service rows; `MaxEventID` uses the same scoping and returns 0 on an
  empty project.
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
