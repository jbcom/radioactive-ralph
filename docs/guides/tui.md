---
title: TUI cockpit
description: Socket-backed plan, worker, and queue controls.
---

# TUI cockpit

`radioactive_ralph tui` opens a socket-backed cockpit for the current repo.
It attaches to the repo service if one is already running; otherwise it
starts one for the session unless `--no-autostart` is set.

## Views

| Tab | Use |
|-----|-----|
| `overview` | Service status, queue counts, active workers |
| `plans` | Active and paused plans, per-plan queue counts, selected plan tasks |
| `ready` | Runnable tasks, including pending tasks whose dependencies are satisfied |
| `approvals` | Tasks waiting for operator approval |
| `blocked` | Tasks blocked on context or operator intervention |
| `running` | Running tasks and provider/session details |
| `failed` | Failed tasks ready for retry, requeue, handoff, or terminal failure |
| `events` | Recent repo-service events |

## Keys

| Key | Action |
|-----|--------|
| `tab`, right arrow | Next tab |
| `shift+tab`, left arrow | Previous tab |
| `j` / down, `k` / up | Move selection |
| `v` | Cycle the variant filter |
| `p` | Cycle the provider filter |
| `c` | Clear active filters |
| `r` | Refresh service status |
| `s` | Stop the repo service |
| `S` | Start the repo service |
| `a` | Approve the selected task on the approvals tab |
| `R` | Requeue the selected task |
| `t` | Retry the selected blocked or failed task |
| `h` | Handoff the selected task; enter `variant[:reason]` |
| `d` | Mark the selected task done |
| `f` | Mark the selected task failed |
| `?` | Toggle the help overlay |
| `q`, `ctrl+c` | Quit |

## Relationship to the CLI

The TUI uses the same plan DAG and repo service as the CLI. Its operator
actions correspond to `radioactive_ralph plan approve`, `requeue`,
`retry`, `handoff`, `mark-done`, and `fail`; it is not a second runtime.
