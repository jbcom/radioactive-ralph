---
title: Getting started
lastUpdated: 2026-04-15
---

# Get started

## Install the binary

| Platform | Command |
|---|---|
| macOS / Linux (Homebrew) | `brew tap jbcom/pkgs https://github.com/jbcom/pkgs && brew install radioactive-ralph` |
| Windows Scoop | `scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph` |
| macOS / Linux curl installer | <code>curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh</code> |

The stable install surface is Homebrew, Scoop, and the curl installer.
Chocolatey is intentionally not advertised for stable releases unless a
future release explicitly adds that package to the supported install
surface.

## Initialize a repo

Run the initializer once at the repo root:

```bash
radioactive_ralph init
```

That creates `.radioactive-ralph/`, writes the repo config files, scaffolds the
human-readable plans directory, and seeds the default provider binding.

## Ask Fixit what should happen first

When you are starting from a plain-English goal, begin with Fixit Ralph:

```bash
radioactive_ralph run --variant fixit --advise \
  --topic finish-runtime-pass \
  --description "finish the runtime pass and queue the next implementation phase"
```

That writes `.radioactive-ralph/plans/<topic>-advisor.md` and seeds the durable
SQLite plan DAG for the repo when the slug does not already exist.

## Start the durable repo service

The durable runtime is the normal way to serve all ten variants:

```bash
radioactive_ralph service start
```

Useful follow-up commands:

```bash
radioactive_ralph status
radioactive_ralph attach
radioactive_ralph tui
radioactive_ralph plan approvals
radioactive_ralph plan blocked
radioactive_ralph plan requeue <plan> <task>
radioactive_ralph plan retry <plan> <task>
radioactive_ralph plan handoff <plan> <task> <variant>
radioactive_ralph plan fail <plan> <task>
radioactive_ralph stop
```

When the runtime asks for operator approval or hands work back for a
different variant, use the plan surface directly:

```bash
radioactive_ralph plan approvals
radioactive_ralph plan tasks <plan>
radioactive_ralph plan approve <plan> <task>
radioactive_ralph plan history <plan> <task>
```

## Run a bounded variant attached to the terminal

Some variants are safe to run as a single attached execution:

```bash
radioactive_ralph run --variant blue
radioactive_ralph run --variant grey
radioactive_ralph run --variant red
radioactive_ralph run --variant fixit
radioactive_ralph run --variant old-man --confirm-no-mercy
```

Long-running variants such as green, professor, immortal, savage, and
world-breaker require the durable repo service.

## Core commands

| Command | What it does |
|---|---|
| `radioactive_ralph init` | Set up `.radioactive-ralph/` and provider config |
| `radioactive_ralph run --variant fixit --advise` | Interpret a free-form goal and seed the durable plan |
| `radioactive_ralph run --variant <name>` | Run a bounded variant attached to the terminal |
| `radioactive_ralph service start` | Launch the durable repo runtime |
| `radioactive_ralph service install` | Register the durable repo runtime with launchd, systemd, or Windows SCM |
| `radioactive_ralph service status` | Report installed service-unit state for the current platform |
| `radioactive_ralph status` | Query the running repo service over the local control plane |
| `radioactive_ralph attach` | Stream repo service events live |
| `radioactive_ralph tui` | Open the repo service cockpit |
| `radioactive_ralph plan approvals` | List tasks waiting for operator approval |
| `radioactive_ralph plan blocked` | List tasks blocked on missing context or operator intervention |
| `radioactive_ralph plan requeue <plan> <task>` | Return a blocked/failed task to the runnable queue |
| `radioactive_ralph plan retry <plan> <task>` | Increment retry count and retry a blocked/failed task |
| `radioactive_ralph plan handoff <plan> <task> <variant>` | Hand a task to a different Ralph persona |
| `radioactive_ralph plan fail <plan> <task>` | Force-fail a task from the operator surface |
| `radioactive_ralph plan approve <plan> <task>` | Approve an approval-gated task |
| `radioactive_ralph plan history <plan> <task>` | Inspect task handoff / approval / completion events |
| `radioactive_ralph stop` | Shut the repo service down gracefully |
| `radioactive_ralph plan ls` | List plans in the durable plan store |

## Current requirements

- `git`
- at least one shipped provider CLI installed and authenticated:
  - `claude`
  - `codex`
  - `gemini`
- `gh` recommended for GitHub workflows

The runtime ships provider bindings for Claude, Codex, and Gemini today. Repo
config picks the default provider and can override it per variant.
