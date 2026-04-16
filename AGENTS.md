---
title: AGENTS.md — radioactive-ralph
lastUpdated: 2026-04-15
---

# Extended Agent Protocols — radioactive-ralph

## Product contract

radioactive-ralph is one binary with three operator faces:

1. **`radioactive_ralph service start`** — the durable repo-scoped runtime.
2. **`radioactive_ralph run --variant <name>`** — attached bounded execution.
3. **`radioactive_ralph tui`** — the socket-backed cockpit.

Do not describe the product as a Claude plugin, an MCP server, or a family of
slash-command skills. Those are no longer part of the live contract.

## Personas

Ralph is one little guy with many personalities.

- Variants are defined in code under `internal/variant/`.
- Voice and flavor live in `internal/voice/`.
- Operator-facing variant docs live under `docs/variants/`.
- Fixit Ralph is the planning bridge personality. When no active plan exists,
  Fixit is the variant that translates a free-form ask into durable DAG state.

## State

radioactive-ralph has two state layers:

- Repo-visible config and planning files under `.radioactive-ralph/`
- Machine-local runtime state under the XDG/App Support root resolved by
  `internal/xdg`

Machine-local state includes:

- `plans.db`
- repo service sockets and PID locks
- runtime logs
- worktrees and mirrors

Never store runtime state under `.claude/`.

## Current command surface

- `radioactive_ralph init`
- `radioactive_ralph run --variant <name>`
- `radioactive_ralph status`
- `radioactive_ralph attach`
- `radioactive_ralph stop`
- `radioactive_ralph tui`
- `radioactive_ralph doctor`
- `radioactive_ralph service <start|install|uninstall|list|status>`
- `radioactive_ralph plan <ls|show|next|tasks|approvals|blocked|approve|requeue|retry|handoff|fail|history|import|mark-done>`

## Provider direction

Today the runtime ships provider bindings for `claude`, `codex`, and `gemini`.
That is the current implementation, not the permanent identity of the system.

The long-term shape is:

- code-defined persona prompts
- repo-declared provider bindings
- support for any compatible CLI provider once prompt/model/effort/output rules
  are defined

When writing docs or code comments, describe Claude as the current shipped
provider, not the permanent boundary of the product.

## Testing patterns

- Use `go test ./...` for the main pass.
- Use `golangci-lint run` for lint.
- Use `python3 -m tox -e docs` for docs build/validation.
- Run `bash scripts/generate-api-docs.sh` when exported Go API surface changes.

## Adding a command

1. Add the command struct under `cmd/radioactive_ralph/`.
2. Implement the backing logic in the relevant `internal/` package.
3. Add or extend tests under `internal/` or `tests/integration/`.
4. Update the live docs in `docs/`.

## PR workflow

- Work on branches and merge through GitHub PRs.
- Prefer squash merges.
- Keep `main` tracking `origin/main` exactly.
- Resolve review threads and keep CI green before merge.
