---
title: Getting started
lastUpdated: 2026-07-16
---

# Get started

## Install the binary

| Platform | Command |
|---|---|
| macOS / Linux (Homebrew) | `brew tap jbcom/pkgs https://github.com/jbcom/pkgs && brew install radioactive-ralph` |
| Windows Scoop | `scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph` |
| macOS / Linux curl installer | <code>curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh</code> |

The stable install surface is Homebrew, Scoop, and the curl installer.

## The two modes

`radioactive_ralph` is one binary that runs in two modes:

- **`radioactive_ralph --supervisor`** — the long-lived supervisor. It owns
  every agent's pty, holds all work open, serves the discovery socket, runs
  the reaper, and owns the one user-level SQLite database that is durable
  memory for every project on the machine. Working directory is irrelevant
  to it.
- **`radioactive_ralph`** (no flag) — the dumb client. It discovers the
  running supervisor and renders a read-only TUI. It refuses to run if no
  supervisor answers.

Start the supervisor once per machine, then run the client from any project
directory.

## Start the supervisor

```bash
radioactive_ralph --supervisor
```

This blocks in the foreground. For daily use, install it as an OS service
instead so it survives logout/reboot/crash — see the
[service runbook](../runbooks/service.md):

```bash
radioactive_ralph service install
radioactive_ralph service status
```

## Initialize a project

From inside a repo (or any directory), register it with the running
supervisor:

```bash
radioactive_ralph --init
```

This identifies the project by accumulated fingerprints (git root-commit +
remote + absolute path, so identity survives `git init` and directory
moves) and stores its config in the one user-level database. Nothing is
written into the repo — no committed config directory, no per-repo
database.

Running plain `radioactive_ralph` in a directory the supervisor doesn't
know about auto-routes to the same initialization, so `--init` is rarely
needed by hand.

## Import a plan

A plan is plain markdown, decomposed heuristically (heading = group,
unordered list = parallel steps, ordered = sequential). Import one to
activate it — the supervisor's periodic dispatch loop then drives its ready
steps:

```bash
radioactive_ralph plan import plan.md
radioactive_ralph plan ls          # confirm it is active
```

A step opts into mechanical, orchestrator-verified completion with an inline
marker: `` `accept: <shell command>` `` (re-run in the project checkout and
must exit 0) or `` `accept-file: <path>` `` (must exist). A step without a
marker is judgment-only. Either way, completion is verified by the runtime,
never inferred from a worker terminating.

## Run the client

```bash
radioactive_ralph
```

In a terminal, this renders the read-only macro/meso/micro TUI showing the
current project's plan and live agent activity. Piped or non-interactive
(CI, `go test`), it prints a single status line instead of launching the
TUI.

## Or use the desktop app

```bash
radioactive_ralph gui
```

The desktop app is a graphical peer to the terminal UI on the same supervisor —
same macro→meso→micro drill, but it can also **drive**: approve a task awaiting
approval, pause/resume/abandon a plan, kill a worker, and import a plan from a
window. Install it with `brew install --cask radioactive-ralph` (macOS), the
Linux `.AppImage`, or the Windows `.exe` from the
[releases page](https://github.com/jbcom/radioactive-ralph/releases/latest);
double-clicking the installed app opens the GUI directly. It opens even before a
supervisor is running and lights up when one appears.

Launched from a project directory, the app scopes to that project; launched by
double-clicking from a file manager (where the working directory isn't a repo)
it opens project-agnostic and lists every project the supervisor knows — it
never registers the launch directory as a new project.

## Check your environment

```bash
radioactive_ralph doctor
```

Reports whether `git`, a supported provider CLI (`claude`, `codex`,
`opencode`), and the platform service manager are available. See
[Provider auth](../runbooks/provider-auth.md) if a provider check fails.

## Current requirements

- `git`
- at least one shipped provider CLI installed and authenticated:
  - `claude`
  - `codex`
  - `opencode`
- `gh` recommended for GitHub workflows

Providers are local-only capability bindings: the CLI owns its own agent
loop and tool execution locally, even when it calls a hosted model for
inference. `gemini` was removed as a shipped provider (CLI deprecated
2026-06-18); `cursor-agent` is excluded because it delegates the session to
Cursor's cloud.
