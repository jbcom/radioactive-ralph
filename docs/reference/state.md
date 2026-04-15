---
title: State
lastUpdated: 2026-04-15
---

# State — radioactive-ralph

This page tracks the live state of the rewrite after the binary-first pivot.

## What changed in the contract

The repo no longer treats the Claude marketplace/plugin path as the main
identity of the product.

The active contract is now:

- `radioactive_ralph` binary first
- Claude Code via stdio MCP
- personas defined in code
- provider abstraction as the target direction

## What is live today

- Go CLI under `cmd/radioactive_ralph/`
- repo init and config scaffolding
- durable SQLite-backed plan store
- stdio MCP server via `radioactive_ralph serve --mcp`
- MCP registration via `radioactive_ralph mcp register`
- service installation commands
- repo-root Sphinx docs
- generated Go API reference under `docs/api/`

## What is now explicitly deprecated

- treating marketplace plugin skills as the main product story
- treating HTTP MCP as a first-class integration path
- describing the personas as external skill files first and code second

Legacy plugin/skill material may still exist in the tree or in archive/history
documents, but it is no longer the architectural center.

## High-priority implementation gaps

### Fixit rewrites are still append-only at the DAG layer

Fixit now seeds the durable DAG on first creation for a topic slug, but reruns
with the same slug refresh the human report and leave the existing DAG plan in
place. Plan revision/replacement semantics are still missing.

### Provider abstraction is still design, not code

The current runtime still shells out to `claude`. The broader provider-binding
model is the next architecture phase, not a shipped feature.

### Safety and gating are still incomplete

Some variant gate and spend-cap behavior exists in profiles/docs but is not yet
fully reflected in the runtime surface.

## What is intentionally true now

- Ralph is one binary with many personalities.
- Claude is a client of the binary, not the other way around.
- Repo config should become the place where future provider bindings live.
- The docs should describe the current product honestly and mark target-state
  material as target-state.
