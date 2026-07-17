---
title: Supervisor and client
description: How --supervisor and the dumb client fit together.
---

# Supervisor and client

`radioactive_ralph` has exactly two runtime roles, chosen by a single flag.

## The supervisor

```bash
radioactive_ralph --supervisor
```

The supervisor is the one long-lived process on a machine. It owns:

- every agent's pty (`creack/pty`) — direct process control, no
  multiplexer in the loop
- the discovery socket (`internal/ipc`) that clients dial
- the reaper, which reclaims stalled or crashed agent processes
- the single user-level SQLite database that is durable memory for every
  registered project

Working directory is irrelevant to the supervisor; it operates at the
user/XDG level. Install it as an OS service for daily use — see the
[service runbook](../runbooks/service.md).

## The client

```bash
radioactive_ralph
```

Plain invocation is a dumb client. It:

- resolves the current directory to a known project (auto-initializing if
  needed)
- discovers the running supervisor over the socket
- renders a read-only Bubble Tea TUI showing the supervisor's live state

It refuses to run if no supervisor answers, and prints the command to
start one. It owns no ptys, no database, and no orchestration logic —
"attach" is the wrong word for this because there is nothing to attach
to or detach from; the client simply shows what the supervisor already
knows.

## Why this shape

A single process must hold every agent's pty for the never-block control
invariant to work: only the process that owns the fd can watch it for
stalls and kill-and-reclaim on the spot. Splitting that ownership across
multiple processes (or a multiplexer) reintroduces exactly the
`os/exec` round-trip and external failure domain the invariant is meant
to remove. The client is deliberately thin so there is only one place
where agent lifecycle and completion-verification logic lives.
