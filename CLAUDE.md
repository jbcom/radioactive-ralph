---
title: CLAUDE.md — radioactive-ralph
updated: 2026-04-14
status: current
---

# radioactive-ralph — Agent Entry Point

Autonomous continuous development orchestrator. Per-repo Go binary that
keeps Claude subprocesses alive, focused, and productive across days of
work.

**Currently mid-architectural-rewrite, now pivoted to Go.** The old Python
tree is at [`reference/`](reference/) and will be deleted at v1.0.0. See
[`docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`](docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md)
for the four-milestone plan.

## Quick orientation (target Go tree — M2 in progress)

```text
cmd/ralph/                         # kong CLI entry point
internal/xdg/                      # state dir + repo-hash helpers
internal/config/                   # kong args + TOML loader + Resolve() with safety floors
internal/inventory/                # shell-based skill/MCP/plugin discovery
internal/variant/                  # Profile + skill biases (M3 fills)
internal/workspace/                # mirror + worktree + LFS (four orthogonal knobs)
internal/db/                       # SQLite + sqlite-vec event log, WAL
internal/ipc/                      # Unix socket server + client
internal/multiplexer/              # tmux / screen / syscall.Setsid fallback
internal/session/                  # ClaudeSession wrapping `claude -p --input-format stream-json`
internal/supervisor/               # per-variant event loop
internal/service/                  # launchd / systemd-user / brew-services integration
internal/initcmd/                  # `radioactive_ralph init` capability-matching wizard
internal/doctor/                   # environment health checks
internal/voice/                    # Ralph personality templates

reference/                         # old Python tree, deleted at v1.0.0
skills/                            # 10 skill MD files (rewritten thin in M3)
docs/                              # hand-written + doc2go-generated API reference
.claude-plugin/marketplace.json    # unchanged from M1
.goreleaser.yaml                   # brew + Scoop + WinGet + tarballs
```

## Commands

```bash
# Go development
go test ./...
go build -o dist/ralph ./cmd/ralph
golangci-lint run
govulncheck ./...
make test          # same as `go test ./...`
make lint          # golangci-lint run + govulncheck
make build         # emits ./dist/ralph

# Release (GoReleaser)
goreleaser release --snapshot --clean   # dry-run, emits ./dist/
git tag v0.6.0 && git push --tags       # triggers real release via GHA

# Runtime (post-M2)
ralph init                              # per-repo setup wizard
ralph run --variant X [--detach]        # launch supervisor
ralph status [--variant X | --all]      # query Unix socket
ralph attach --variant X                # stream events
ralph stop [--variant X]                # graceful shutdown
ralph doctor                            # environment health
ralph service install --variant X       # emit launchd/systemd unit
```

## What radioactive-ralph is NOT

- An MCP server acting as a live bridge between an outer Claude session
  and the daemon. Confirmed impossible in Claude Code 2026 —
  interactive sessions have no IPC channel for external user-message
  injection.
- A general-purpose task runner, tmux replacement, or SaaS orchestrator.
- A replacement for human judgment on vision and direction.
- A multi-operator coordination tool (one operator per daemon).
- A non-git workspace tool.
- A multi-LLM-provider framework (Anthropic only).
- A code reviewer, PR classifier, work-discoverer, or forge API client.
  Claude already does all of these inside worktree sessions when given
  the right skills; the daemon does not duplicate them.

## Critical rules

- **Config lives in-repo**: `.radioactive-ralph/config.toml` (committed)
  + `.radioactive-ralph/local.toml` (gitignored). Missing config =
  refuse to run.
- **State lives in XDG**: `$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/`
  on Linux/WSL; `~/Library/Application Support/radioactive-ralph/
  <repo-hash>/` on macOS. Never in `.claude/`, never in the repo.
- **SSH remotes only** — `git@github.com:`, never `https://`.
- **Conventional commits** — `feat:`, `fix:`, `chore:`, `docs:`,
  `refactor:`, etc.
- **stream-json is the session protocol** — daemon spawns
  `claude -p --input-format stream-json --output-format stream-json`
  and pipes user messages to stdin. Never interactive mode for managed
  sessions.
- **Mirror-based workspaces** for `mirror-*` isolation variants —
  worktrees are created off a `git clone --mirror` in XDG, not off the
  operator's repo.
- **No git ops in the daemon** except `git worktree add/remove` and
  `git fetch` for mirror management. All other git work (commits, PRs,
  merges, branch hygiene, history rewrites) happens inside worktree
  Claude sessions.
- **No forge API clients, no code review logic, no PR classifier** in
  the daemon. Claude does these in the worktree using whatever skills
  the operator has installed.
- **Capability inventory drives bias injection** — variant profiles
  declare preferred skill categories (review, security review, docs
  query). The operator picks preferences during `radioactive_ralph init`. The
  supervisor injects bias snippets into each managed session's system
  prompt based on actual installed inventory.
- **Safety floors are non-negotiable** — destructive variants'
  `object_store = full` and confirmation gates cannot be weakened by
  single-flag override.
- **Go file size limit**: 300 lines per `.go` file (same global limit
  as the Python 300-LOC rule).

## Docs

Published at <https://jonbogaty.com/radioactive-ralph/> via GitHub
Pages. During the Python era the docs were Sphinx + AutoAPI; during
the Go rewrite they're being migrated to hand-written markdown + doc2go
for generated API reference. See `docs/` for the current state.

## Release

No code signing in initial releases. Single cross-packager monorepo at
`jbcom/pkgs` holds Formula/, bucket/, choco/, etc. — users tap with
`brew tap jbcom/pkgs`. Install script at
`jonbogaty.com/radioactive-ralph/install.sh`. Windows via Scoop/WinGet
for the binary; the supervisor runs only in POSIX environments so
Windows users use WSL2+Linuxbrew for the full experience.
