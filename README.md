<h1 align="center">radioactive-ralph</h1>

<p align="center">
  <img src="https://raw.githubusercontent.com/jbcom/radioactive-ralph/main/assets/brand/ralph-mascot.png" alt="Radioactive Ralph mascot" width="400"/>
</p>

<p align="center">
  <em>A supervised-execution runtime for local AI-agent CLIs.</em>
</p>

<p align="center">
  <a href="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml"><img src="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <a href="https://jonbogaty.com/radioactive-ralph/"><img src="https://img.shields.io/badge/docs-jonbogaty.com%2Fradioactive--ralph-22c55e" alt="Docs"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"/></a>
</p>

radioactive-ralph runs your local AI-agent CLIs (Claude, Codex, OpenCode) as
**supervised workers that can never block**. It installs one executable that,
in supervisor mode, owns each agent's pseudo-terminal, watches every worker for
stalls and permission-prompts, and kills-and-reclaims instead of ever waiting.
Work is driven by a simple markdown plan, decomposed with pure heuristics (no
LLM), and a step is only "done" once the runtime **verifies** it — never
because an agent said so.

> **Rewrite in progress.** The project is being rebuilt to this supervisor
> architecture. The authoritative design is
> [docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md](./docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md);
> the decision trail is `.agent-state/decisions.ndjson`.

## What it is

- **One binary, two modes.** `radioactive_ralph --supervisor` is the long-lived
  process that owns every agent's pty and holds all work open. Plain
  `radioactive_ralph` is a **dumb client**: it discovers the supervisor over a
  socket at an XDG path and renders a read-only view — it refuses to run without
  a supervisor.
- **The control invariant.** An agent CLI can never block the system. Agents run
  non-interactively under Ralph's pty; a watchdog surfaces any stall or
  permission-prompt and the runtime kills-and-reclaims. Recovery is cheap
  because state is durable.
- **One user-level database.** A single SQLite DB (in your XDG data dir) is
  durable memory for **all** projects. Repos stay clean — no committed config
  dir, no per-repo database. Projects are recognized by accumulated fingerprints
  (git root-commit, remote, path), so identity survives `git init` and moves.
- **No personas.** There are no variants. One mutating Ralph; behavior comes
  from the task and its context, not roleplay.
- **Markdown plans, verified completion.** Plans are plain markdown decomposed
  heuristically (heading = group, unordered list = parallel steps, ordered =
  sequential). The orchestrator dispatches steps with scoped context and
  verifies each against its acceptance criteria before marking it done.
- **Local-only providers.** `claude`, `codex`, `opencode` — the agent loop and
  tool execution run locally (hosted model inference is fine). A2A coordination
  vocabulary comes from the official [`a2aproject/a2a-go`](https://github.com/a2aproject/a2a-go).

## Install

| Platform | Command |
|---|---|
| macOS / Linux (Homebrew) | `brew tap jbcom/pkgs https://github.com/jbcom/pkgs && brew install radioactive-ralph` |
| Windows Scoop | `scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph` |
| macOS / Linux curl installer | <code>curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh</code> |

## Quick start

```bash
# 1. Start the supervisor (owns everything; user/XDG-level, directory-independent)
radioactive_ralph --supervisor        # or install it as a system service

# 2. In a project directory, initialize it (registers the project in the user DB)
radioactive_ralph --init

# 3. Import a markdown plan; it is activated and the supervisor begins driving it
radioactive_ralph plan import plan.md

# 4. Run the client to see live status / the read-only cockpit
radioactive_ralph
```

The client refuses to run unless a supervisor is reachable, and tells you how to
start one. Nothing is written into your repository.

## CLI surface

```bash
radioactive_ralph --supervisor      # run the supervisor (owns agent ptys + the user DB)
radioactive_ralph                   # dumb client: discover the supervisor, read-only view
radioactive_ralph --init            # initialize / re-initialize the current project
radioactive_ralph plan import <f>   # import a markdown plan and activate it
radioactive_ralph plan ls [--all]   # list the current project's plans
radioactive_ralph doctor            # environment checks
```

## Docs

- [Getting started](https://jonbogaty.com/radioactive-ralph/getting-started/)
- [Architecture](https://jonbogaty.com/radioactive-ralph/reference/architecture/)
- [Design spec](./docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md)

## Contributing

See [AGENTS.md](./AGENTS.md) (the canonical agent protocol) and
[STANDARDS.md](./STANDARDS.md).

```bash
go build ./...
go test ./...
go test -race ./...
golangci-lint run
python3 -m tox -e docs
```

## License

MIT. See [LICENSE](https://github.com/jbcom/radioactive-ralph/blob/main/LICENSE).
