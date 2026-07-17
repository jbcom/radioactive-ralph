---
title: STANDARDS.md — radioactive-ralph
lastUpdated: 2026-07-16
---

# Code Standards — radioactive-ralph

## Non-negotiable constraints

- **300 LOC max per file** — split if needed
- **Go is the live product implementation** — the runtime, CLI, TUI, provider layer, and service integration all live in Go
- **Keep the Go toolchain green** — `go test ./...` and `golangci-lint run` must pass
- **Keep the docs release surface green** — `python3 -m tox -e docs` must pass when docs or exported Go APIs change
- **Refresh generated API docs when exported surface changes** — run `bash scripts/generate-api-docs.sh`
- **No prelaunch compatibility theater** — remove dead surfaces or mark them archival explicitly
- **Never reintroduce live MCP/plugin/skill framing by accident** — archive it or call it out as historical if it must remain referenced

## Commit format

Conventional Commits always:
```
feat: add repo-service approval actions
fix: handle stale socket heartbeat cleanly
chore: update deps
docs: add architecture diagram
```

## Git

- SSH remotes only: `git@github.com:jbcom/radioactive-ralph.git`
- Never force push
- Always squash merge PRs
- Keep `main` matching `origin/main`

## Security

- Never log API keys or tokens
- Use argument-vector subprocess execution; never shell-inject untrusted strings
- All project/plan/config/spend state lives in the one user-level SQLite
  database under the XDG/App Support state root — never a committed
  per-repo config or database
- Never store runtime state under `.claude/`

## Product Contract

- `radioactive_ralph --supervisor` is the durable supervisor: it owns
  every agent's pty, the discovery socket, the reaper, and the one
  user-level database
- Plain `radioactive_ralph` is a dumb, read-only client that refuses to
  run without a live supervisor
- `radioactive_ralph --init` registers a project by accumulated
  fingerprints, never by committed repo state
- `radioactive_ralph service {install,uninstall,status}` manages the
  supervisor as a per-user OS service
- Providers are bindings, not the identity of the product
- There are no variants/personas — one mutating Ralph, driven by the plan
