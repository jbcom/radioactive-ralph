---
title: Getting started
lastUpdated: 2026-04-15
---

# Get started

## Install the binary

| Platform | Command |
|---|---|
| macOS / Linuxbrew / WSL2+Linuxbrew | `brew tap jbcom/pkgs && brew install radioactive-ralph` |
| Windows Scoop | `scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph` |
| Windows Chocolatey | `choco install radioactive-ralph` |
| POSIX curl installer | `curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh` |

## Initialize a repo

Run the initializer at the repo root once:

```bash
radioactive_ralph init
```

That creates `.radioactive-ralph/`, writes the repo config files, scaffolds
`plans/index.md`, and registers `radioactive_ralph` with Claude Code as a
stdio MCP server unless you pass `--skip-mcp`.

## Ask Fixit what should happen first

When you are starting from a plain-English goal, begin with Fixit Ralph:

```bash
radioactive_ralph run --variant fixit --advise \
  --topic finish-docs-pass \
  --description "finish the docs pass and line up the next implementation phase"
```

Today that writes an advisor report to `.radioactive-ralph/plans/<topic>-advisor.md`
and, on first creation for that repo/topic slug, syncs the recommendation into
the durable plan DAG every other Ralph depends on.

## Launch a working persona

Once you have plan context, run the persona you actually want:

```bash
radioactive_ralph plan ls
radioactive_ralph run --variant green --foreground
```

Useful follow-up commands:

```bash
radioactive_ralph status --variant green
radioactive_ralph attach --variant green
radioactive_ralph stop --variant green
```

## Core commands

| Command | What it does |
|---|---|
| `radioactive_ralph init` | Set up `.radioactive-ralph/` and register Claude MCP |
| `radioactive_ralph run --variant fixit --advise` | Interpret a free-form goal, write the advisor report, and seed the durable plan |
| `radioactive_ralph run --variant <name>` | Launch a supervisor for a specific Ralph persona |
| `radioactive_ralph status --variant <name>` | Query a running supervisor over its Unix socket |
| `radioactive_ralph attach --variant <name>` | Stream supervisor events live |
| `radioactive_ralph stop --variant <name>` | Shut a supervisor down gracefully |
| `radioactive_ralph plan ls` | List plans in the durable plan store |
| `radioactive_ralph serve --mcp` | Run the stdio MCP server |
| `radioactive_ralph mcp register` | Register the stdio MCP server with Claude Code |

## Current requirements

- `git`
- `claude` CLI installed and authenticated
- `gh` recommended for GitHub workflows

Claude is the current provider implementation. The product direction is broader:
binary-first, persona-first, and eventually provider-agnostic.
