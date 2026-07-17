---
title: Desktop GUI
description: The Fyne desktop client — a graphical peer to the TUI that can also drive.
---

# Desktop GUI

`radioactive_ralph gui` (or double-clicking the installed desktop app) opens the
graphical client — a peer to the terminal [TUI](./tui.md) on the same supervisor
socket. It shows the same live macro→meso→micro drill, but unlike the read-only
TUI it can also **drive**: approve a task awaiting approval, pause/resume/abandon
a plan, kill a worker, and import a plan — all from a window.

The GUI is a Go-native [Fyne](https://fyne.io) application with one visual
identity across terminal and desktop: it shares the TUI's semantic status
palette (`internal/statusbucket`), so a `running` task is the same cyan and a
`paused` plan the same orange whether you are in a terminal or a window.

## Installing it

The `gui` command opens a window only in a **GUI-enabled build** — the desktop
app installs. The CLI-only installs ship the terminal client and print a note
when you run `gui` there.

| Platform | Install |
|---|---|
| macOS | `brew install --cask radioactive-ralph` (opens cleanly — no Gatekeeper prompt) |
| Linux | the `.AppImage` from the [latest release](https://github.com/jbcom/radioactive-ralph/releases/latest) — `chmod +x`, run |
| macOS (direct) / Windows | the `.dmg` / `.exe` from the latest release |

Everything is signed the open-source way — no paid Apple or Microsoft
credentials — so the macOS cask needs no security override. (The Windows `.exe`
is Authenticode-signed once the project's free SignPath enrollment is
configured; until then Windows SmartScreen may warn on first launch.)

## Launching

- **From a terminal:** `radioactive_ralph gui`.
- **By double-clicking** the installed `.app` / AppImage / `.exe`: the binary
  detects that it was launched from a desktop context (no controlling terminal)
  and opens the GUI rather than the TUI.
- **Project scope:** launched from a project directory, the GUI scopes to that
  project; launched from a file manager (working directory not a repo) it opens
  project-agnostic and lists every project the supervisor knows — it never
  registers the launch directory as a new project.

The window opens even before a supervisor is running: the header shows
`waiting for supervisor…`, and it lights up to `connected · up <duration>` the
moment one appears.

## The three drill levels

| Level | Shows | Drive actions |
|---|---|---|
| Macro | Status header + the project's plans (status chip + progress) + a "Recent activity" feed of recent project events | Import a plan |
| Meso | One plan's tasks with per-task status | Pause / Resume / Abandon the plan; Approve a task awaiting approval |
| Micro | One task's event timeline | Kill the worker running the task |

Selecting a plan drills to meso; selecting a task drills to micro; the back
buttons return. It is the same live snapshot the supervisor holds — no separate
client-side state to drift.

## How driving stays safe

Every drive action funnels through the supervisor over the versioned IPC drive
API — the supervisor remains the single writer of record, exactly as when the
CLI imports a plan. The GUI never spawns or talks to an agent CLI directly, and
killing a worker uses the same kill-and-reclaim path a watchdog kill would, so
the control invariant (an agent can never block the system) is never bypassed.

All of the GUI's reads and drive calls run off the UI thread, so a slow or
unavailable supervisor can stale the view but never freeze the window.

## Relationship to the CLI and TUI

One binary. Plain `radioactive_ralph` is the read-only TUI; `radioactive_ralph
gui` is the drive-capable desktop peer. Both discover the same supervisor and
show the same state — pick whichever surface fits the moment.
