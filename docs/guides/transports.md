---
title: Runtime surfaces
description: How the repo service, attached run, and TUI fit together.
---

# Runtime surfaces

radioactive-ralph has three operator-facing runtime surfaces.

## Durable repo service

```bash
radioactive_ralph service start
```

This is the main runtime. It owns:

- the durable SQLite plan DAG session
- task claiming and progression
- worktrees and mirrors
- provider subprocess execution
- the local control plane used by the CLI and TUI

Use this mode when you want the full system, including long-running variants.

## Attached bounded run

```bash
radioactive_ralph run --variant blue
```

This mode is for bounded variants only. It uses the same underlying runtime
engine, but it stays attached to the current terminal and exits when the
eligible work is done.

If a durable repo service is already running, attached `run` refuses to start a
competing runtime.

## TUI cockpit

```bash
radioactive_ralph tui
```

The TUI is a socket-backed cockpit. It attaches to the repo service when it is
already running, or launches it if it is absent. It shows repo status,
plan/task queues, blocked work, running workers/providers, recent event flow,
and direct operator actions.

## Why the model changed

The old product shape tried to make multiple things primary at once. The live
contract is cleaner:

- the binary is the product
- the repo service is the runtime
- the TUI and CLI are clients of that runtime
- providers are backends, not the boundary of the system
