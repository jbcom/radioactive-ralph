---
title: CLAUDE.md — radioactive-ralph
lastUpdated: 2026-04-15
---

# radioactive-ralph — Agent Entry Point

radioactive-ralph is a repo-scoped runtime for AI-assisted software work.

## Core shape

- `radioactive_ralph service start` runs the durable repo service.
- `radioactive_ralph run --variant <name>` runs one bounded variant attached to
  the current terminal.
- `radioactive_ralph tui` is the socket-backed cockpit.
- Fixit Ralph is the only variant that should translate a free-form user ask
  into durable plan state when no active plan exists.

## Current tree

```text
cmd/radioactive_ralph/           # CLI entry point and subcommands
internal/config/                 # repo config + local overrides
internal/db/                     # event log
internal/doctor/                 # environment checks
internal/fixit/                  # fixit planning pipeline
internal/initcmd/                # repo bootstrap
internal/ipc/                    # socket protocol and client/server
internal/plandag/                # durable SQLite plan DAG
internal/provider/               # provider binding abstraction
internal/runtime/                # durable repo service
internal/service/                # platform service integration
internal/provider/claudesession/ # Claude-backed provider session runner
internal/variant/                # Ralph persona profiles
internal/voice/                  # Ralph flavor text
internal/workspace/              # mirrors and worktrees
internal/xdg/                    # machine-local state paths
docs/                            # repo-root Sphinx docs
```

## Commands

```bash
go test ./...
golangci-lint run
python3 -m tox -e docs

radioactive_ralph init
radioactive_ralph run --variant <name>
radioactive_ralph status
radioactive_ralph attach
radioactive_ralph stop
radioactive_ralph tui
radioactive_ralph service start
radioactive_ralph service install
radioactive_ralph service uninstall
radioactive_ralph service list
radioactive_ralph service status
radioactive_ralph plan ls
radioactive_ralph plan show <id-or-slug>
radioactive_ralph plan next <id-or-slug>
radioactive_ralph plan tasks <id-or-slug>
radioactive_ralph plan approvals
radioactive_ralph plan blocked
radioactive_ralph plan approve <plan> <task>
radioactive_ralph plan requeue <plan> <task>
radioactive_ralph plan retry <plan> <task>
radioactive_ralph plan handoff <plan> <task> <variant>
radioactive_ralph plan fail <plan> <task>
radioactive_ralph plan history <plan> <task>
radioactive_ralph plan import <path>
radioactive_ralph plan mark-done <plan> <task>
```

## Rules

- Config lives in `.radioactive-ralph/`.
- Durable runtime state lives under the XDG/App Support root, never under
  `.claude/`.
- Providers are bindings, not the identity of the product.
- The shipped providers are `claude`, `codex`, and `gemini`; future providers
  should fit the same prompt/model/effort/result contract.
- Destructive variants still require explicit confirmation gates and spend-cap
  enforcement where declared.

## Docs

Published at <https://jonbogaty.com/radioactive-ralph/> from repo-root `docs/`.
Generated API reference comes from `gomarkdoc` into `docs/api/`.
