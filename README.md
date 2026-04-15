<h1 align="center">radioactive-ralph</h1>

<p align="center">
  <img src="https://raw.githubusercontent.com/jbcom/radioactive-ralph/main/assets/brand/ralph-mascot.png" alt="Autonomous continuous development orchestrator for Claude Code." width="400"/>
</p>

<p align="center">
  <em>Autonomous continuous development orchestrator for Claude Code.</em>
</p>

<p align="center">
  <a href="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml"><img src="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <a href="https://jonbogaty.com/radioactive-ralph/"><img src="https://img.shields.io/badge/docs-jonbogaty.com%2Fradioactive--ralph-22c55e" alt="Docs"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"/></a>
</p>

radioactive-ralph drives Claude Code across a portfolio of git repos — continuously, with a sense of humor, and with enough structure to keep the funny little guy from burning the school down.

## Current status

The live implementation is now the **Go binary** under
`cmd/radioactive_ralph`, with repo-root Sphinx docs under [`docs/`](docs/).
The earlier Python tree is preserved under [`reference/`](reference/) as
historical context while the remaining rewrite work finishes.

Current shipped surface includes:

- the repo initializer and config scaffolding
- the per-repo supervisor CLI
- the durable SQLite-backed plan DAG
- MCP registration plus stdio/HTTP transports
- launchd/systemd service installation
- generated Go API reference in the docs site

Roadmap details still live in the
[PRD](docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md).

## What it is

| Mode | What you get | Best for |
|---|---|---|
| Claude Code plugin (skills) | Ten Ralph variants — each a slash command with its own safety profile, with `fixit-ralph` acting as the plan/advisor entry point when no valid initialized plan exists | In-session invocation where you want variant-specific behavior without leaving Claude Code |
| `radioactive_ralph` binary (CLI) | `radioactive_ralph init` then `radioactive_ralph run --variant fixit --advise` — turns a free-form operator ask into a real plan, then runs the supervisor, plan tooling, services, and MCP surface directly | Long-running orchestration, repo initialization, plan bootstrap, MCP registration, and explicit operator control |
| System service | `radioactive_ralph service install --variant green` — launchd on macOS, systemd --user on Linux, brew-services wrapped either way | Always-on autonomous operation for green, immortal, or blue variants |

## Meet the Ralphs

| Variant | Specialty | Use it when | Gate |
|---|---|---|---|
| [`/green-ralph`](https://jonbogaty.com/radioactive-ralph/variants/green-ralph/) | The classic loop | You want the default full-power orchestrator | — |
| [`/grey-ralph`](https://jonbogaty.com/radioactive-ralph/variants/grey-ralph/) | Cheap mechanical cleanup | You need governance docs and boring hygiene fast | — |
| [`/red-ralph`](https://jonbogaty.com/radioactive-ralph/variants/red-ralph/) | CI and PR fire drills | Something is on fire and you want one clean report | — |
| [`/blue-ralph`](https://jonbogaty.com/radioactive-ralph/variants/blue-ralph/) | Read-only review | You want diagnosis without touching the code | — |
| [`/professor-ralph`](https://jonbogaty.com/radioactive-ralph/variants/professor-ralph/) | Plan → execute → reflect | Strategy matters more than speed | — |
| [`/fixit-ralph`](https://jonbogaty.com/radioactive-ralph/variants/fixit-ralph/) | Advisor + ROI-scored bursts | You need a free-form ask translated into initialized plan context, or small budget-conscious work | — |
| [`/immortal-ralph`](https://jonbogaty.com/radioactive-ralph/variants/immortal-ralph/) | Recovery-first autonomy | You need it to survive the night | — |
| [`/savage-ralph`](https://jonbogaty.com/radioactive-ralph/variants/savage-ralph/) | Maximum throughput | Budget is not the constraint | `--confirm-burn-budget` |
| [`/old-man-ralph`](https://jonbogaty.com/radioactive-ralph/variants/old-man-ralph/) | Imposed target state | Negotiation is over | `--confirm-no-mercy` |
| [`/world-breaker-ralph`](https://jonbogaty.com/radioactive-ralph/variants/world-breaker-ralph/) | Every agent on opus | The problem is genuinely catastrophic | `--confirm-burn-everything` |

See the full [variants index](https://jonbogaty.com/radioactive-ralph/variants/) for bios, arguments, and safety profiles.

## Install

Current release paths:

```bash
# Homebrew (macOS / Linuxbrew / WSL2 + Linuxbrew)
brew tap jbcom/pkgs
brew install radioactive-ralph

# Windows Scoop
scoop bucket add jbcom https://github.com/jbcom/pkgs
scoop install radioactive-ralph

# Windows Chocolatey
choco install radioactive-ralph

# curl | sh (POSIX)
curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh
```

To expose Ralph to Claude Code as an MCP server after installing the binary:

```bash
radioactive_ralph mcp register
```

If you also want the slash-command packaging in Claude Code:

```bash
claude plugin marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive_ralph@jbcom-plugins
```

The supervisor itself is still POSIX-first. Native Windows install is mainly
for packaging parity and MCP/client workflows; long-running supervisor use is
best through WSL2 or another POSIX environment.

## Current CLI surface

```bash
radioactive_ralph init
radioactive_ralph run --variant fixit --advise --topic "stabilize docs and CI"
radioactive_ralph plan ls
radioactive_ralph run --variant green --foreground
radioactive_ralph status --variant green
radioactive_ralph attach --variant green
radioactive_ralph stop --variant green
radioactive_ralph doctor
radioactive_ralph service install --variant green
radioactive_ralph service list
radioactive_ralph plan ls
radioactive_ralph plan show bootstrap
radioactive_ralph serve --mcp
radioactive_ralph mcp register
```

## Docs and design system

- [Getting started](https://jonbogaty.com/radioactive-ralph/getting-started/)
- [Ralph variants](https://jonbogaty.com/radioactive-ralph/variants/)
- [Architecture reference](https://jonbogaty.com/radioactive-ralph/reference/architecture/)
- [Launch guide](https://jonbogaty.com/radioactive-ralph/guides/launch/)

## Requirements

- `claude` CLI installed and authenticated (`claude login`)
- `gh` CLI installed and authenticated (`gh auth login`)
- `git` ≥ 2.5 (for worktrees)
- `tmux` strongly recommended (the supervisor falls through to `screen` or `setsid` if not)

## Contributing

See [AGENTS.md](./AGENTS.md), [STANDARDS.md](./STANDARDS.md), and [CONTRIBUTING guidance in the docs](https://jonbogaty.com/radioactive-ralph/reference/testing/).

```bash
git clone git@github.com:jbcom/radioactive-ralph.git
cd radioactive-ralph
make test          # go test ./...
make lint          # golangci-lint run
make build         # build radioactive_ralph binary into ./dist/
```

## License

MIT. See [LICENSE](https://github.com/jbcom/radioactive-ralph/blob/main/LICENSE).
