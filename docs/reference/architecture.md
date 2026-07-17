---
title: Architecture
lastUpdated: 2026-07-16
---

# Architecture

## One binary, two modes

- **`radioactive_ralph --supervisor`** — the long-lived supervisor. It
  owns every agent subprocess's pty (`creack/pty`, `internal/agent`),
  holds all work open, serves the discovery socket (`internal/ipc`), runs
  the reaper, and owns the one user-level SQLite database
  (`internal/store`). Working directory is irrelevant to it.
- **`radioactive_ralph`** (no flag) — a dumb client. It discovers the
  supervisor, resolves the current directory to a known project
  (auto-initializing if needed), and renders a read-only Bubble Tea TUI.
  It refuses to run without a live supervisor.
- **`radioactive_ralph --init`** — explicitly registers or re-registers
  the current directory as a project.
- **`radioactive_ralph service {install,uninstall,status}`** — manages
  the supervisor as a per-user OS service (launchd/systemd/Windows SCM)
  so it survives logout/reboot/crash.
- **`radioactive_ralph doctor`** — environment checks (git, provider
  CLIs, service manager).

There are no other command-level subsystems: no variant/persona
selection, no per-repo plan commands, no attach/cockpit framing. The
client *is* the read-only view; there's nothing separate to attach to.

## The control invariant

An agent CLI must never block the system. The supervisor owns each
agent's pty directly and runs a watchdog that classifies stream output as
progress, stall (no-output-for-N), or an interactive-prompt pattern —
any of which triggers auto-resolve, deny, or kill-and-reclaim. See
[Safety floors](../guides/safety-floors.md).

## Discovery

The supervisor binds a Unix domain socket (a named pipe on Windows) at a
well-known path under the XDG runtime state root
(`ipc.ServiceEndpoint`: `<state-root>/service.sock`, heartbeat
`service.sock.alive`). Discovery is just dialing that socket:

- success → a supervisor is live
- failure → refuse, and print the command to start one

Single-instance is enforced by an exclusive flock on a PID lockfile, not
by the socket bind — the PID lock plus heartbeat file distinguish a live
supervisor from a stale socket left by a crashed one (dead PID → the next
supervisor reclaims: remove the stale socket, take over).

## State: one user-level database, clean repos

All project, plan, config, and spend state lives in **one user-level
SQLite database** under the XDG data root — durable memory for every
registered project on the machine, not per-repo. There is no committed
config directory and no per-repo database. Never store Ralph runtime
state under `.claude/`.

### Project identity

A project is identified by **accumulated fingerprints**, not an absolute
path:

- a git directory fingerprints via git heuristics (root-commit sha,
  remote, repo-root markers)
- a non-git directory seeds with its absolute path

Identifiers accumulate: a directory that starts as path-only and is later
`git init`-ed gains its git fingerprints on top of the existing path
identifier, so the same project stays recognized across that transition
and across directory moves.

## Config

Configuration resolves through virtual layers built by the supervisor
from the database plus three override flags
(`--config-file`/`-C`, `--user-config-file`, `--project-config-file`).
See [Config virtual layers](../design/config-layers.md) for the full
model.

## Plans and completion

Plans are markdown, decomposed heuristically over a `goldmark` AST — no
LLM in decomposition. The orchestrator dispatches ready steps to agent
workers with plan-scoped context and verifies completion against
acceptance criteria; a worker's self-report or process termination is
never sufficient on its own. See [Plan format](../guides/plan-format.md)
and [Orchestrator-verified completion and
A2A](../design/completion-and-a2a.md).

## Providers

Shipped providers: `claude`, `codex`, `opencode` — each a capability
record, not a persona. "Local-only" means the CLI owns its own agent loop
and tool execution locally, even when it calls a hosted model for
inference. `gemini` was removed (CLI auth endpoint deprecated
2026-06-18); `cursor-agent` is excluded (delegates session control to
Cursor's cloud). See [Provider contract](../design/provider-contract.md).
