---
title: MCP transports — stdio vs HTTP
description: When to pick each transport and how to switch between them.
---

Radioactive Ralph exposes its plan + variant tool surface as an MCP
server. The server supports two transports — **stdio** and **streamable
HTTP** — and you pick one at registration time. This page explains the
tradeoffs and shows the exact `claude mcp add` / `radioactive_ralph mcp
register` commands for each.

## TL;DR

| | **stdio** | **HTTP** |
|--|--|--|
| Lifetime | Per-session (spawned by Claude) | Long-lived daemon |
| Management | Nothing — Claude owns it | `launchd` / `systemd` / `brew services` |
| Multi-session | One MCP server per Claude session | Shared by N sessions |
| Setup | Zero config | Pick a port + service |
| Default? | **Yes** | No |

Unless you have a reason to run Ralph as a durable service, **stay on
stdio**. It has zero operational surface.

## stdio (default)

Claude Code spawns `radioactive_ralph serve --mcp` as a subprocess and
pipes JSON-RPC over its stdin/stdout. One process per Claude session;
the process dies when the session dies. Parent-death is handled by the
lifeline pipe in `internal/variantpool`.

Register with:

```sh
radioactive_ralph mcp register            # default: stdio, user scope
```

which shells out to:

```sh
claude mcp add --scope user radioactive-ralph -- radioactive_ralph serve --mcp
```

`radioactive_ralph init` registers this automatically when you run it
in a new repo (`--skip-mcp` disables the auto-register step).

## HTTP (streamable)

One long-running `radioactive_ralph serve --mcp --http :7777` process
accepts HTTP POSTs carrying JSON-RPC, and multiple Claude sessions
share it. This matches the durable-service pattern from the design doc
— use it when you want a single supervisor watching one plan DB across
sessions, or when you've wired Ralph into `launchd` / `systemd` /
`brew services`.

Register with:

```sh
radioactive_ralph mcp register --transport http \
  --http-addr http://localhost:7777/mcp
```

which shells out to:

```sh
claude mcp add --transport http --scope user \
  radioactive-ralph http://localhost:7777/mcp
```

Then start the server (foreground or under `brew services`):

```sh
radioactive_ralph serve --mcp --http :7777
```

### Running HTTP mode durably

```sh
radioactive_ralph service install --variant <name>
```

emits a `launchd` plist (macOS) / `systemd` user unit (Linux) / brew
services formula for the given variant. That installs a supervisor unit
for the variant; it does **not** automatically turn on the HTTP MCP
listener for you.

If you want durable HTTP mode, run the MCP server explicitly:

```sh
radioactive_ralph serve --mcp --http :7777
```

and manage that process separately.

## Switching transports

Registration + service are independent — you can unregister one
transport and register the other without restarting anything else.

```sh
radioactive_ralph mcp unregister           # drops current entry
radioactive_ralph mcp register --transport http
```

Claude picks up the new registration on next session.

## Scope

Both transports accept `--scope local | user | project`:

- **local** — this repo only, ephemeral (`.claude.json` in cwd)
- **user** — all repos for this user (default; writes `~/.claude.json`)
- **project** — checked into the repo (`.mcp.json`)

Use `project` scope for repos where every contributor should get Ralph
automatically; use `user` scope for personal defaults.
