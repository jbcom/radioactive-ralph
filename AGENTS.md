---
title: AGENTS.md — radioactive-ralph
updated: 2026-04-15
status: current
domain: technical
---

# Extended Agent Protocols — radioactive-ralph

## Architecture

radioactive-ralph currently ships as a **Go binary** with two practical entry
paths:

1. **`radioactive_ralph` CLI** — the repo-scoped supervisor, MCP server, plan
   tooling, doctor checks, and service installation surface all live under
   `cmd/radioactive_ralph/`.
2. **Claude Code skills / plugin packaging** — the slash-command variants in
   `skills/` and `.claude-plugin/` are the interactive front door, but they
   target the same current Go-era Ralph surface rather than the archived Python
   implementation.

The old Python daemon is preserved under `reference/` as historical context.
Treat it as archive material, not the live implementation.

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
  - contains the per-repo workspace, `state.db`, sockets, logs, and the
    durable plan DAG SQLite store

Never store runtime state under `.claude/`.

## Current Command Surface

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

If documentation mentions the old discover / PR-list / dashboard command family
or Python-era daemon commands, treat it as stale unless it is clearly marked as
archive material.

## Fixit Ralph

`fixit-ralph` is the only Ralph variant that can bridge a user-directed ask
into the live plan system. It is the plan/advisor specialist:

- when plans are missing or malformed, `fixit-ralph --advise` inspects the
  repo, interprets the operator prompt, and writes a repo-visible advisor report
- when executable tasks need to exist in the durable store, fixit is the
  variant that understands how to translate that intent into the live SQLite
  plan DAG workflow
- every other variant depends on that plans-first discipline and should defer to
  fixit when no valid initialized plan context exists

## Testing Patterns

- Use `go test ./...` for the main test pass.
- Use `make test`, `make lint`, and `make vuln` for the standard repo targets.
- Use `python3 -m tox -e docs` for docs validation + Sphinx build.
- Run `bash scripts/generate-api-docs.sh` when exported Go API surface changes.

## Adding A Command

1. Add the command struct under `cmd/radioactive_ralph/`.
2. Implement the backing logic in the relevant `internal/` package.
3. Add or extend tests under `internal/` or `tests/integration/`.
4. Update the live docs in `docs/` and regenerate API reference if exported
   surface changed.

## PR Workflow

- Work on branches and merge through GitHub PRs.
- Prefer squash merges.
- Keep `main` tracking `origin/main` exactly; delete merged topic branches.
- Resolve review threads and keep CI green before merge.
