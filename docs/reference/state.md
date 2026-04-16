---
title: State
lastUpdated: 2026-04-15
---

# State

This page tracks the live state of the runtime after the repo-service pivot.

## What is live today

- Go CLI under `cmd/radioactive_ralph/`
- repo init and config scaffolding
- durable SQLite-backed plan store
- durable repo-scoped runtime under `radioactive_ralph service start`
- attached bounded execution under `radioactive_ralph run --variant <name>`
- socket-backed `status`, `attach`, `stop`, and `tui`
- operator task controls via `plan tasks`, `plan approvals`, `plan blocked`, `plan approve`, `plan requeue`, `plan retry`, `plan handoff`, `plan fail`, and `plan history`
- named provider bindings with a repo-level default provider
- shipped provider runners for `claude`, `codex`, and `gemini`
- native Windows durable-service support via SCM + named pipes
- repo-root Sphinx docs
- generated Go API reference under `docs/api/`

## What changed

The live contract no longer includes:

- MCP serving
- plugin packaging as a product surface
- per-variant supervisors
- detached multiplexer management

Those concepts may still appear in archived plan documents, but they are not
part of the shipped runtime anymore.

## Remaining work

The remaining work is polish rather than missing architecture:

- richer TUI navigation, filtering, and prompt-entry ergonomics
- broader native-host smoke testing, especially on real Windows machines
- continued copy cleanup in archival and lore-heavy pages that intentionally
  preserve older design history

## What is intentionally true now

- Ralph is one binary with many personalities.
- The durable repo service is the main runtime.
- Attached `run` exists for bounded variants only.
- Fixit is the bridge from free-form human ask to durable plan context.
- Providers are bindings, not the identity of the product.
