---
title: Architecture
lastUpdated: 2026-04-15
---

# Architecture — radioactive-ralph

This page describes the **current architecture direction** for the live product.
The implementation still has gaps, but the contract is now clearer than it was:
radioactive-ralph is a binary-first tool with in-code personas, and Claude Code
is treated as a client of that binary over stdio MCP.

## Core commitment

The repo is no longer trying to be all of these at once:

- a Claude marketplace plugin
- a family of slash-command skills
- a binary
- a durable HTTP MCP service
- a provider-agnostic runtime

The active direction is narrower:

1. **One binary** — `radioactive_ralph`
2. **One durable state model** — repo config plus XDG/App Support runtime state
3. **One Claude integration path** — stdio MCP
4. **One persona source of truth** — code-defined Ralph variants

## Product shape

| Layer | Role |
|---|---|
| `radioactive_ralph` binary | The product boundary: init, plan store, supervisor, doctor, and MCP server |
| Claude Code MCP | Structured control plane into the binary |
| Variant profiles | Built-in Ralph personas that shape system prompt, safety posture, and runtime behavior |
| Provider bindings | Future abstraction for non-Claude agent CLIs |

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
- `local.toml` is gitignored operator-local state.
- `plans/` contains the human-readable planning artifacts.

## Machine-local state

Machine-local runtime state lives under the Ralph state root:

```text
$XDG_STATE_HOME/radioactive-ralph/
└── <repo-hash>/
    ├── plans.db
    ├── sessions/
    ├── logs/
    └── worktrees/
```

This is where the durable plan DAG, runtime sessions, and per-repo transient
state belong. Never store this under `.claude/`.

## Claude integration

Claude Code is integrated through stdio MCP:

- `radioactive_ralph init` registers the binary with `claude mcp add`
- Claude spawns `radioactive_ralph serve --mcp`
- the MCP server exposes plan and runtime control surface
- the binary remains the authority over variant behavior and state

The abandoned complexity was trying to make plugin skills, service-managed HTTP,
and binary control all equally primary. They are not.

## Personas, not skills

Ralph has many personalities, but the source of truth is now the code:

- `internal/variant/` defines the behavioral profiles
- `internal/voice/` defines the Ralph voice layer
- `docs/variants/` explains each personality to operators

This keeps the canon in one place. A persona may eventually be surfaced through
different front ends, but it should not require a separate parallel skill spec
to exist.

## Current provider reality

Today the runtime still assumes the `claude` CLI for agent execution. The code
therefore still knows about Claude-specific concerns such as model tier names,
reasoning effort, and CLI session behavior.

That is **current implementation**, not the long-term contract.

## Provider direction

The target shape is a declarative provider binding layer inside repo config.
Conceptually that means each repo can say:

- which agent CLI to invoke
- how to set model
- how to set effort
- how to append the Ralph persona prompt
- how to pass the task/user prompt
- what structured output format to expect back

That would allow the same Ralph personas to drive Claude, Codex, Gemini, OpenAI
CLI tooling, or any other provider with the necessary bindings.

## Current gaps

- Fixit still writes markdown advice but does not yet fully populate the live DAG.
- Plan gating is not yet properly repo-scoped.
- `run` still exposes less safety/runtime wiring than the variant docs imply.
- Provider bindings are still a design direction, not live code.

See [state](./state.md) for the explicit status of those gaps.
