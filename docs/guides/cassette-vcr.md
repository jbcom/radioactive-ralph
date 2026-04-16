---
title: Cassette VCR — deterministic replay of Claude sessions
description: How internal/provider/claudesession/cassette records + replays claude stream-json so tests run hermetically without API credentials.
---

The session wrapper (`internal/provider/claudesession`) talks to a `claude -p
--input-format stream-json` subprocess. Real subprocess interactions
are non-deterministic — they need credentials, the API can time out,
and responses vary between calls — which makes them unsuitable for
CI.

`internal/provider/claudesession/cassette` solves this with a VCR-style record/replay
layer: capture a real session once, replay its I/O deterministically
forever.

## Architecture

Cassettes are subprocess-level, not HTTP-level. Claude makes its own
API calls inside the subprocess; we never see the HTTP traffic.
Cassettes therefore capture the observable I/O of the claude binary:

- **stdin** — user messages the session wrote to claude
- **stdout** — stream-json frames claude wrote back
- **timing** — relative timestamps between frames

On-disk format is JSON so diffs are readable in code review:

```json
{
  "version": 1,
  "recorded_at": "2026-04-12T10:15:00Z",
  "args": ["--input-format", "stream-json"],
  "events": [
    {"t": 0.00, "dir": "in",  "frame": {...}},
    {"t": 1.23, "dir": "out", "frame": {...}}
  ]
}
```

## Recording

Recording requires a working Claude Code install (so the real claude
binary can authenticate via the operator's credentials). Typical
pattern inside a test:

```go
rec, err := cassette.NewRecorder(cassettePath, realClaudeBin, args)
if err != nil { t.Fatal(err) }
defer rec.Close()

sess, _ := session.Spawn(session.Options{
    ClaudeBin: rec.BinPath(),  // recorder wraps the real binary
    Args:      args,
})
// drive the session normally; rec.Close() flushes the JSON
```

## Replaying

The replayer is a tiny standalone binary at
`internal/provider/claudesession/cassette/replayer`. It reads the cassette and emits
the recorded stdout frames with their recorded timing. Point the
session at it and it looks like real claude, minus the credentials:

```go
os.Setenv("RALPH_CASSETTE_PATH", cassettePath)

sess, _ := session.Spawn(session.Options{
    ClaudeBin: cassette.ReplayerPath,  // built test-side
    Args:      args,
})
```

## When to re-record

Re-record when:

- The stream-json schema changes (claude adds/removes fields)
- The test scenario changes (you want a different conversation)
- The recording has drifted from reality (unlikely — the cassette
  should be stable across claude versions for the same args)

Don't re-record for minor assertion tweaks — those belong in test
code, not cassette data.

## Where cassettes live

Tests that use cassettes keep them alongside the test file:

```text
internal/provider/claudesession/
├── session.go
├── session_test.go
└── testdata/
    └── basic_ping.cassette.json
```

`testdata/` is the Go convention — `go test` excludes it from build
paths but `embed.FS` can pull it in at test time. Tests that need a
cassette fail loudly (not skip) if the file is missing, so a stale
test suite is visible immediately.

## Writing a new test with a cassette

1. Write the test using the replayer. Point it at a cassette path
   that doesn't exist yet.
2. Run the test — it'll fail with "cassette not found".
3. Switch to the recorder wrapper, run once against real claude with
   credentials, commit the generated `.cassette.json`.
4. Switch back to the replayer. The test now runs hermetically.

The recorder + replayer share the same on-disk schema (see
`cassette.go::Cassette`), so steps 3 → 4 are a one-line flip in the
test.
