<h1 align="center">radioactive-ralph</h1>

<p align="center">
  <img src="https://raw.githubusercontent.com/jbcom/radioactive-ralph/main/assets/brand/ralph-mascot.png" alt="Radioactive Ralph mascot" width="400"/>
</p>

<p align="center">
  <em>A helpful little guy with a lot of personalities.</em>
</p>

<p align="center">
  <a href="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml"><img src="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <a href="https://jonbogaty.com/radioactive-ralph/"><img src="https://img.shields.io/badge/docs-jonbogaty.com%2Fradioactive--ralph-22c55e" alt="Docs"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"/></a>
</p>

radioactive-ralph is a binary-first repo runtime for AI-assisted development.
It installs one executable, keeps one durable SQLite plan DAG, and lets you
pick from ten built-in Ralph personas that control how the little guy thinks,
acts, and spends effort.

## Product contract

- `radioactive_ralph service start` is the durable repo runtime.
- `radioactive_ralph run --variant <name>` is the attached bounded runner.
- `radioactive_ralph tui` is the socket-backed cockpit.
- Fixit Ralph is the planning bridge when a repo has no active plan.
- Providers are configured in `.radioactive-ralph/config.toml` with a
  repo-level `default_provider` and named `[providers.<name>]` blocks.

The v1 shipped providers are `claude`, `codex`, and `gemini`. The runtime
model is broader: Ralph personas live in code, and repositories can bind any
compatible CLI provider once the prompt/model/effort/output contract is
defined.

## Install

| Platform | Command |
|---|---|
| macOS / Linux (Homebrew) | `brew tap jbcom/pkgs https://github.com/jbcom/pkgs && brew install radioactive-ralph` |
| Windows Scoop | `scoop bucket add jbcom https://github.com/jbcom/pkgs && scoop install radioactive-ralph` |
| Windows Chocolatey | `choco install radioactive-ralph` |
| macOS / Linux curl installer | <code>curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh</code> |

## Start a repo

```bash
radioactive_ralph init
radioactive_ralph run --variant fixit --advise \
  --topic stabilize-runtime \
  --description "stabilize the runtime and queue the next implementation pass"
radioactive_ralph plan ls
radioactive_ralph plan approvals
radioactive_ralph service start
```

If you want a bounded attached run instead of the durable service:

```bash
radioactive_ralph run --variant blue
radioactive_ralph run --variant grey
radioactive_ralph run --variant red
radioactive_ralph run --variant fixit
radioactive_ralph run --variant old-man --confirm-no-mercy
```

Variants with infinite or long-running behavior require the durable service.

## Current CLI surface

```bash
radioactive_ralph init
radioactive_ralph run --variant <name>
radioactive_ralph status
radioactive_ralph attach
radioactive_ralph stop
radioactive_ralph tui
radioactive_ralph doctor
radioactive_ralph service start
radioactive_ralph service install
radioactive_ralph service uninstall
radioactive_ralph service list
radioactive_ralph service status
radioactive_ralph plan ls
radioactive_ralph plan show <id-or-slug>
radioactive_ralph plan next <id-or-slug>
radioactive_ralph plan tasks <id-or-slug>
radioactive_ralph plan approvals
radioactive_ralph plan blocked
radioactive_ralph plan requeue <plan> <task>
radioactive_ralph plan retry <plan> <task>
radioactive_ralph plan handoff <plan> <task> <variant>
radioactive_ralph plan fail <plan> <task>
radioactive_ralph plan approve <id-or-slug> <task-id>
radioactive_ralph plan history <id-or-slug> <task-id>
radioactive_ralph plan import <path>
radioactive_ralph plan mark-done <id-or-slug> <task-id>
```

## Runtime modes

| Surface | Role |
|---|---|
| `service start` | Durable repo-scoped runtime over the local control plane |
| `run --variant <name>` | Attached bounded execution for safe, finite variants |
| `tui` | Cockpit that attaches to the repo service or launches it if absent |

## Docs

- [Getting started](https://jonbogaty.com/radioactive-ralph/getting-started/)
- [Variants](https://jonbogaty.com/radioactive-ralph/variants/)
- [Architecture](https://jonbogaty.com/radioactive-ralph/reference/architecture/)
- [Implementation state](https://jonbogaty.com/radioactive-ralph/reference/state/)

## Contributing

See [AGENTS.md](/Users/jbogaty/src/jbcom/radioactive-ralph/AGENTS.md),
[STANDARDS.md](/Users/jbogaty/src/jbcom/radioactive-ralph/STANDARDS.md), and
[docs/reference/testing.md](/Users/jbogaty/src/jbcom/radioactive-ralph/docs/reference/testing.md).

```bash
go test ./...
golangci-lint run
python3 -m tox -e docs
```

## License

MIT. See [LICENSE](https://github.com/jbcom/radioactive-ralph/blob/main/LICENSE).
