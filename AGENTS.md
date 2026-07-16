---
title: AGENTS.md — radioactive-ralph
lastUpdated: 2026-07-16
---

# Extended Agent Protocols — radioactive-ralph

Read `CLAUDE.md` first for the core shape, and the authoritative design at
`docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md`.

## Product contract

radioactive-ralph is one binary that runs in two modes:

1. **`radioactive_ralph --supervisor`** — the long-lived supervisor: owns
   every agent subprocess's pty, holds all work open, serves the control
   socket, runs the reaper, and owns the one user-level SQLite DB.
2. **`radioactive_ralph`** (no flag) — the **dumb client**: discovers the
   supervisor via its socket, initializes project config, and renders a
   read-only view. It refuses to run without a supervisor.

Do not describe the product as a Claude plugin, an MCP server, or a family of
slash-command skills. There are **no variants/personas** — that model was
removed. There is one mutating Ralph; behavior comes from the plan and the
task context, not a persona.

## The control invariant (non-negotiable)

An agent CLI must NEVER block the system — no permission prompts, no
clarification waits, no interactive menus. Agents run non-interactively under
Ralph's own pty (`internal/agent`, via `creack/pty`); the watchdog surfaces
any stall/prompt/no-output and the runtime auto-resolves, denies, or
kills-and-reclaims. Kill is cheap because state is durable (the plan slice in
the DB), so recovery is replaying that slice to a fresh worker.

## State — one user-level SQLite DB, clean repos

All project/plan/config/worker/spend state lives in **one user-level SQLite
DB** under the XDG data root (`internal/store`, opened by the supervisor).
There is NO committed repo state — no `.radioactive-ralph/` dir, no per-repo
DB. Projects are identified by accumulated fingerprints (git root-commit +
remote + abs-path), so identity survives `git init` and directory moves.
Never store runtime state under `.claude/`.

## Command surface

- `radioactive_ralph --supervisor` — run the supervisor.
- `radioactive_ralph` — dumb client (discover + read-only view).
- `radioactive_ralph --init` — initialize/re-initialize the current project.
- `radioactive_ralph doctor` — environment checks.

Config resolves through virtual layers (`internal/vconfig`, cobra/viper):
three flags (`--config-file`/`-C`, `--user-config-file`, `--project-config-file`),
USER layer = DB < `--config-file` < `--user-config-file`, PROJECTS layer =
all-DB-projects < the user config's `projects:` stanza.

## Providers — local-only capability bindings

Shipped providers: `claude`, `codex`, `opencode`. "Local-only" means the CLI
owns the agent loop + tool execution + session control locally (hosted model
inference is fine). `gemini` removed (CLI deprecated 2026-06-18);
`cursor-agent` excluded (delegates the session to Cursor's cloud). Each
provider binding is a **capability record** (`internal/provider/binding.go`),
including a `NativeFanout` flag for CLIs that natively fan out subagents/
workflows — NOT a persona. Detection/classification is `internal/agentdetect`.
A2A coordination vocabulary is the official `a2aproject/a2a-go`.

## Plans + completion

Plans are simple markdown decomposed heuristically over the goldmark AST
(`internal/plan`): heading = group, unordered list = parallel steps, ordered =
sequential, don't descend past a heading with subheadings. No LLM in
decomposition. The orchestrator (`internal/orch`) dispatches steps with
plan-scoped context and **verifies completion against acceptance criteria** —
completion is never agent-asserted and never inferred from termination.

## Testing patterns

- `go build ./...` must compile; `go test ./...` for the main pass;
  `go test -race ./...` for concurrency-touching packages.
- `golangci-lint run` for lint (gofmt-clean; the repo's gci convention merges
  third-party + internal imports into one block after stdlib).
- `python3 -m tox -e docs` for the docs build.
- Run `bash scripts/generate-api-docs.sh` when the exported Go API changes.
- Each new package lands build/test/-race/lint-green in isolation (the rewrite
  proceeds phase-by-phase per the implementation plan).

## Adding a command / provider / package

1. CLI commands: add a cobra command under `cmd/radioactive_ralph/`, back it
   with logic in the relevant `internal/` package.
2. Providers: add a runner + a capability `BindingConfig` and register it in
   `internal/provider` (table-driven); document how Ralph speaks to it.
3. Keep `internal/store` a leaf; the supervisor sits on top; the client is
   dumb. No import cycles.

## PR workflow

- Work on branches; merge through GitHub PRs; prefer squash merges.
- Keep `main` tracking `origin/main` exactly; branch protection requires
  Test/Lint/Build + conversation resolution.
- Resolve review threads and keep CI green before merge.
