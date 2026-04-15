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

## Initialize a repo

Run the initializer at the repo root once:

```bash
radioactive_ralph init
```

That creates `.radioactive-ralph/`, seeds the bootstrap plan scaffolding, and registers the MCP server with Claude Code unless you pass `--skip-mcp`.

## Ask fixit what to do first

When you're starting from a plain-English goal instead of an existing plan,
start with Fixit Ralph:

```bash
radioactive_ralph run --variant fixit --advise --topic "finish the next docs pass"
```

That writes `.radioactive-ralph/plans/<topic>-advisor.md` and gives the rest of
the variant system a concrete initialized plan context to work from.

## Launch the recommended variant

Once fixit has translated the ask into a plan, launch the working variant you
actually want to run. For a first foreground run:

```bash
radioactive_ralph plan ls
radioactive_ralph run --variant green --foreground
```

Useful follow-up commands after a variant is running:

```bash
radioactive_ralph status --variant green
radioactive_ralph attach --variant green
radioactive_ralph stop --variant green
```

Inside Claude Code, the normal entry point for a free-form ask is Fixit Ralph:

```text
/fixit-ralph
```

## Core commands

| Command | What it does |
|---|---|
| `radioactive_ralph init` | Set up `.radioactive-ralph/` and capability selections for the current repo |
| `radioactive_ralph run --variant fixit --advise` | Interpret a free-form goal and write the advisor plan other variants can follow |
| `radioactive_ralph run --variant <name>` | Launch a supervisor for a specific Ralph variant once valid plan context exists |
| `radioactive_ralph status --variant <name>` | Query a running supervisor over its Unix socket |
| `radioactive_ralph attach --variant <name>` | Stream supervisor events live |
| `radioactive_ralph stop --variant <name>` | Shut a supervisor down gracefully |
| `radioactive_ralph plan ls` | List plans in the local plan store |
| `radioactive_ralph serve --mcp` | Run the MCP server in stdio mode |
| `radioactive_ralph mcp register` | Register the MCP server with Claude Code |

## Requirements

- Git
- `claude` CLI installed and authenticated
- `gh` CLI recommended for GitHub workflows
- `tmux` or `screen` recommended if you use detached runs
