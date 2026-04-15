---
title: Claude MCP integration
description: How Claude Code talks to radioactive_ralph today.
---

# Claude MCP integration

Claude Code currently integrates with radioactive-ralph through **stdio MCP**.
That is the active, supported contract.

## Why stdio

The repo previously drifted into supporting both stdio and HTTP in the docs and
in the CLI surface. That created unnecessary ambiguity:

- Who owns runtime state?
- Is the product the binary or the HTTP server?
- Are Claude plugin add-ons the real entry point or not?

The answer now is simpler:

- the **binary** is the product
- Claude is a **client**
- stdio MCP is the **integration path**

## What `init` does

By default, `radioactive_ralph init` runs:

```bash
claude mcp add --scope user radioactive_ralph -- radioactive_ralph serve --mcp
```

That means the next Claude session can spawn the binary as an MCP server and
use its plan/runtime tools directly.

## Manual registration

If you skipped registration during init, run:

```bash
radioactive_ralph mcp register
```

Use `--scope local`, `--scope user`, or `--scope project` depending on whether
the registration should be repo-local, personal, or checked into the repo via
`.mcp.json`.

## What Claude should think of Ralph as

Not a plugin bundle. Not an HTTP service. Not a second product.

Claude should treat `radioactive_ralph` as:

- a repo-local planning and orchestration binary
- a set of structured MCP tools backed by that binary
- a helpful little guy with many built-in personalities

## Current limitation

The runtime is still Claude-CLI-backed under the hood. Provider-agnostic
bindings are the next design phase, but the Claude integration model should
already look like what future providers will use: one binary, one structured
tool surface, one source of truth.
