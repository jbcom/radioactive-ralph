---
title: AGENTS.md — radioactive-ralph
lastUpdated: 2026-04-15
---

# Extended Agent Protocols — radioactive-ralph

## Product contract

radioactive-ralph is a **binary-first** orchestration tool.

The live product is:

1. **`radioactive_ralph` CLI** — repo init, plan storage, supervisor launch,
   doctor checks, MCP serving, and service installation all live under
   `cmd/radioactive_ralph/`.
2. **Claude Code via MCP** — Claude is treated as a client of the binary, not
   the product boundary. `radioactive_ralph init` registers stdio MCP so Claude
   can inspect plans and control Ralph through the binary.

Do not write or review docs as though the main product is a Claude marketplace
plugin or a family of slash-command skills. That is no longer the active
direction.

## Personas

Ralph is one little guy with many personalities.

- Variants are defined in code under `internal/variant/`.
- Voice and flavor live in `internal/voice/`.
- The operator-facing narrative for each personality lives in `docs/variants/`.
- Fixit Ralph is the planning bridge personality: when a repo lacks usable plan
  context, fixit is the variant that should interpret a free-form ask and
  convert it into the real plan flow.

## State

radioactive-ralph has two state layers:

- **Repo-visible config and planning files**
  - `.radioactive-ralph/config.toml`
  - `.radioactive-ralph/local.toml`
  - `.radioactive-ralph/plans/index.md`
  - `.radioactive-ralph/plans/*-advisor.md`
- **Machine-local runtime state**
  - `$RALPH_STATE_DIR` if set
  - otherwise the XDG/App Support root resolved by `internal/xdg`
  - includes `plans.db`, sockets, logs, and per-repo runtime state

Never store runtime state under `.claude/`.

## Current command surface

The live CLI is:

- `radioactive_ralph init`
- `radioactive_ralph run --variant <name>`
- `radioactive_ralph status --variant <name>`
- `radioactive_ralph attach --variant <name>`
- `radioactive_ralph stop --variant <name>`
- `radioactive_ralph doctor`
- `radioactive_ralph service <install|uninstall|list|status>`
- `radioactive_ralph plan <ls|show|next|import|mark-done>`
- `radioactive_ralph serve --mcp`
- `radioactive_ralph mcp <register|unregister|status>`

If documentation presents the product as a Claude plugin, a family of slash
commands, or an HTTP MCP service first, treat that as stale unless it is
clearly marked as archive or target-state design.

## Provider direction

Today the runtime targets the `claude` CLI directly. The intended long-term
shape is broader:

- code-defined persona prompts
- declarative provider bindings in repo config
- support for any compatible agent CLI once prompt/model/effort/output bindings
  are defined

When writing docs or code comments, describe Claude as the **current provider**,
not the permanent identity of the system.

## Testing patterns

- Use `go test ./...` for the main test pass.
- Use `make test`, `make lint`, and `make vuln` for the standard repo targets.
- Use `python3 -m tox -e docs` for docs validation and Sphinx build.
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
