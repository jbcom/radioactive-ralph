---
title: CLAUDE.md — radioactive-ralph
lastUpdated: 2026-07-16
---

# radioactive-ralph — Claude entry point

This file is the Claude-specific pointer. The architecture, product contract,
state model, provider rules, and workflow are **canonical in
[AGENTS.md](AGENTS.md)** — read it first. Do not duplicate that content here;
keep this file to Claude-specific pillars and links.

## Pillars — load these into every session

- **[AGENTS.md](AGENTS.md)** — the shared, tool-agnostic agent protocol
  (supervisor + dumb client, the never-block control invariant, one user-level
  SQLite DB / clean repos, no variants, markdown plans, local-only providers,
  testing + PR workflow). Canonical.
- **[Architecture spec](docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md)**
  — the authoritative design. Read before non-trivial work.
- **[Implementation plan](docs/superpowers/plans/2026-07-16-supervisor-architecture.md)**
  — the phased rewrite plan.
- **`.agent-state/decisions.ndjson`** — the append-only decision trail with the
  rationale behind every load-bearing call. Consult before re-litigating a
  decision; append to it when you make one.
- **`.agent-state/directive.md`** — the durable work queue (Status + checkbox
  items). The stop-hook keeps the loop driving while it is ACTIVE with
  unchecked, non-wait items.

## Claude-specific notes

- One-liner: radioactive-ralph is a **supervised-execution runtime for local
  AI-agent CLIs** — Claude/Codex/OpenCode run non-interactively under Ralph's
  own pty and can never block the system.
- The current tree, commands, and package map live in [AGENTS.md](AGENTS.md).
- When acting as an executor on this rewrite: each task lands
  `go build ./...` + `go test` (+ `-race` where relevant) + `golangci-lint`
  green in isolation before the next; commit per task; do not weaken tests to
  pass.
