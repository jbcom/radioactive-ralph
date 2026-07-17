# `radioactive_ralph events` — headless live event tail

**Status:** accepted (2026-07-17). Depends on the merged attach event stream
(#169: `Client.AttachEvents`, `store.MaxEventID`/`ListProjectEvents`).

## Problem

The supervisor's Attach event stream (`Client.AttachEvents`) has GUI/TUI
consumers but **no command-line consumer**. A user scripting Ralph, watching a
run in CI, or debugging can't observe the live event flow without opening the
TUI/GUI — and `Client.AttachEvents` (the typed observe API) has no caller at
all. There is no `radioactive_ralph events` / `logs` command.

## Goal

Add `radioactive_ralph events`: tail the current project's live events to
stdout, one line per event, until interrupted (Ctrl-C / ctx cancel). It is the
headless peer of the GUI/TUI live view — the observe half of the drive+observe
API, usable from a shell or a CI assertion. This also gives
`Client.AttachEvents` its first production caller.

Non-goals: no filtering DSL, no TUI rendering, no historical pagination beyond
an optional backlog seed, no follow-across-restart (a supervisor restart ends
the stream; the user re-runs). YAGNI — a plain, pipeable tail.

## Behavior

`radioactive_ralph events [--backlog N] [--json]`

- Resolves the current project (`ensureProjectKnown`, same as `plan ls`) and
  finds the supervisor (`supervisor.Find`); errors cleanly with the shared
  no-supervisor message if none is listening.
- **Backlog:** `--backlog N` (default 0) first prints the N most recent existing
  events (via the store's `ListProjectEvents`, oldest-first for readability),
  then continues live. The live cursor is seeded to the max id observed in the
  backlog read (or `store.MaxEventID` when `N==0`), so no event is missed or
  duplicated across the backlog→live boundary — the same client-owned-cursor
  rule the GUI/TUI use.
- **Live:** subscribes via `Client.AttachEvents(ctx, ipc.AttachArgs{ProjectID,
  AfterID: cursor}, fn)`; `fn` prints each event and returns nil. The stream
  runs until the user interrupts (the root command's ctx is already wired to
  SIGINT) or the supervisor closes it.
- **Format:** default is a compact human line —
  `<time> <kind> [task=<id>] [actor=<actor>]` — mirroring the TUI's
  `renderEvent`. `--json` emits each `ipc.AttachEvent` as one JSON object per
  line (JSONL), for machine consumption.

## Layering

- **cmd** (`cmd/radioactive_ralph/events_cmd.go`, new): `newEventsCmd()` cobra
  command + `runEvents`. Registered in `main.go` alongside `newPlanCmd()`. Reuses
  `ensureProjectKnown`, `storeDBPath`, `supervisor.Find`. Opens the store only
  for the backlog read + cursor seed, then closes it before the (long-lived)
  attach so the CLI holds no DB handle while idle-tailing.
- No change to `internal/ipc`, `internal/store`, or the supervisor — the command
  is a pure consumer of the existing API.

## Error handling

- No supervisor → the shared `errNoSupervisorListening`.
- A backlog/cursor read error is fatal before the stream starts (nothing to tail
  from a broken store).
- `AttachEvents` returning an error mid-stream (supervisor closed / gone) prints
  a one-line notice to stderr and exits non-zero, so a CI wrapper sees the drop;
  a clean end (nil) exits zero. Ctx cancel (Ctrl-C) exits zero.

## Testing

- A `runEvents` unit test against a fake that supplies a scripted event stream
  and a backlog, asserting: backlog printed oldest-first then live frames; the
  live cursor equals the backlog's max id (no gap/dup); `--json` emits valid
  JSONL; a mid-stream error exits non-zero. Factor the socket/store wiring behind
  a small seam (an interface with `Backlog`/`MaxEventID`/`AttachEvents`, real
  impl over store+client, fake in the test) so the command logic is testable
  without a live supervisor — matching how `gui`/`tui` use a Controller/DataSource.
- `main_test.go`: the command is registered and `--help` renders.

## Why this is the right next feature

It puts the observe half of the drive+observe API to real headless use, gives
`Client.AttachEvents` its first production caller, and is self-contained (one
command, no core change) — high utility for scripting/CI/debugging, contained
blast radius.
