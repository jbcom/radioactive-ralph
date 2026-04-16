---
title: Copilot Instructions
lastUpdated: 2026-04-15
---

See [CLAUDE.md](../CLAUDE.md), [AGENTS.md](../AGENTS.md), and
[STANDARDS.md](../STANDARDS.md) for the full repo contract.

Key rules:

- Keep files under the repo's 300 LOC discipline where practical.
- Treat Go as the live implementation surface for the runtime, CLI, TUI,
  provider layer, and platform service integration.
- Keep `go test ./...`, `golangci-lint run`, and `python3 -m tox -e docs`
  green.
- Use SSH remotes.
- Never store runtime state under `.claude/`.
- Do not reintroduce live MCP, plugin, or slash-command skill framing into
  current docs or code comments.
