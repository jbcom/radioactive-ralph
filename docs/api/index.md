---
title: Go API reference
description: Auto-generated Go package documentation.
---

This section is **generated** from Go doc comments via
[gomarkdoc](https://github.com/princjef/gomarkdoc). Do not edit
files under `docs/api/` directly. Changes will be
overwritten on the next build.

To improve this reference, edit the doc comments in the corresponding
`.go` file and regenerate via `make docs-api` from the repo root.

## Organization

The reference mirrors the Go source tree:

- **cmd/radioactive_ralph/** — CLI entry points and subcommand handlers
- **internal/** — everything else — config, session, supervisor, fixit,
  variant, multiplexer, IPC, service, workspace, etc.

Each package page lists constants, variables, functions, types, and
their public methods with signatures and associated doc comments.

```{toctree}
:hidden:
cmd/radioactive_ralph
internal/config
internal/db
internal/doctor
internal/fixit
internal/initcmd
internal/inventory
internal/ipc
internal/mcp
internal/multiplexer
internal/plandag
internal/plandag/schema
internal/proclife
internal/rlog
internal/service
internal/session
internal/session/cassette
internal/session/cassette/replayer
internal/session/internal/fakeclaude
internal/supervisor
internal/variant
internal/variantpool
internal/voice
internal/workspace
internal/xdg
```
