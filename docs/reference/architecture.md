---
title: Architecture
lastUpdated: 2026-04-15
---

# Architecture

This page describes the current live architecture.

## Core commitment

radioactive-ralph is no longer trying to be all of these at once:

- a plugin marketplace package
- an MCP service
- a slash-command bundle
- a detached multiplexer wrapper

The live product is narrower and cleaner:

1. **One binary** — `radioactive_ralph`
2. **One durable state model** — repo config plus XDG/App Support runtime state
3. **One repo runtime** — the durable service over the local control plane
4. **One persona source of truth** — code-defined Ralph variants

## Runtime surfaces

| Surface | Role |
|---|---|
| `radioactive_ralph service start` | Durable repo-scoped runtime |
| `radioactive_ralph run --variant <name>` | Attached bounded execution |
| `radioactive_ralph tui` | Cockpit that attaches to the repo service |

The local RPC layer is the real control plane: Unix sockets on macOS/Linux,
named pipes on Windows. The TUI and CLI both speak to the same repo-local
runtime.

## Repo-visible state

Every repo that uses Ralph has `.radioactive-ralph/` alongside `.git/`:

```text
.radioactive-ralph/
├── config.toml
├── .gitignore
├── local.toml
└── plans/
    ├── index.md
    └── <topic>-advisor.md
```

- `config.toml` is committed repo policy.
- `local.toml` is gitignored operator-local override state.
- `plans/` contains the human-readable planning artifacts.

## Machine-local state

Machine-local runtime state lives under the Ralph state root:

```text
$XDG_STATE_HOME/radioactive-ralph/
├── plans.db
└── <repo-hash>/
    ├── state.db
    ├── sessions/
    ├── logs/
    └── worktrees/
```

The global `plans.db` stores the durable DAG. The per-repo hash workspace stores
runtime sessions, repo service sockets, logs, event state, and worktrees. Never
store Ralph runtime state under `.claude/`.

## Variant execution policy

Variants are defined in `internal/variant/` and now declare whether they are:

- allowed in attached mode
- allowed in durable service mode

The rule is:

- bounded variants can run attached
- all ten variants can run under the durable service

That keeps the operator model simple without flattening the personalities into
one generic loop.

## Provider layer

Today the runtime ships provider bindings for the `claude` and `codex`
CLIs. (A third built-in, `gemini`, was removed on 2026-06-18 after the
Gemini CLI's auth endpoint was deprecated; the declarative provider path
still allows binding a self-hosted gemini-compatible CLI.)
The repo config now carries:

- `default_provider`
- `[providers.<name>]`
- optional per-variant `provider = "<name>"` overrides

The runtime owns task retrieval, prompt composition, result parsing, and DAG
updates. Providers are just execution backends.
