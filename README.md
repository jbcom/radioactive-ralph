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

## Under active rewrite

radioactive-ralph is mid-architectural-pivot. The project started as a Python package; it is being rewritten as a Go binary. See the [PRD](docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md) for the four-milestone plan. The old Python tree is preserved in [`reference/`](reference/) until the Go rewrite ships v1.0.0.

Status: **M1 merged** (marketplace hygiene, broken implementations stubbed, docs aligned to target architecture). **M2 in progress** (Go skeleton, capability-matching init wizard, SQLite event log, Unix socket IPC, stream-json session control, mirror-based worktree orchestration, brew/launchd/systemd service integration).

## What it is

| Mode | What you get | Best for |
|---|---|---|
| Claude Code plugin (skills) | Ten Ralph variants — each a slash command that launches the daemon in the background and returns control to the outer session | In-session invocation, the skill handles pre-flight checks and hand-off |
| `ralph` binary (CLI) | `ralph init` then `ralph run --variant X` — runs the orchestrator directly outside any Claude session | Long-running orchestration, multi-day autonomous work on a codebase |
| System service | `ralph service install --variant green` — launchd on macOS, systemd --user on Linux, brew-services wrapped either way | Always-on autonomous operation for green, immortal, or blue variants |

## Meet the Ralphs

| Variant | Specialty | Use it when | Gate |
|---|---|---|---|
| [`/green-ralph`](https://jonbogaty.com/radioactive-ralph/variants/green-ralph/) | The classic loop | You want the default full-power orchestrator | — |
| [`/grey-ralph`](https://jonbogaty.com/radioactive-ralph/variants/grey-ralph/) | Cheap mechanical cleanup | You need governance docs and boring hygiene fast | — |
| [`/red-ralph`](https://jonbogaty.com/radioactive-ralph/variants/red-ralph/) | CI and PR fire drills | Something is on fire and you want one clean report | — |
| [`/blue-ralph`](https://jonbogaty.com/radioactive-ralph/variants/blue-ralph/) | Read-only review | You want diagnosis without touching the code | — |
| [`/professor-ralph`](https://jonbogaty.com/radioactive-ralph/variants/professor-ralph/) | Plan → execute → reflect | Strategy matters more than speed | — |
| [`/joe-fixit-ralph`](https://jonbogaty.com/radioactive-ralph/variants/joe-fixit-ralph/) | ROI-scored bursts | You want small, budget-conscious, reviewable work | — |
| [`/immortal-ralph`](https://jonbogaty.com/radioactive-ralph/variants/immortal-ralph/) | Recovery-first autonomy | You need it to survive the night | — |
| [`/savage-ralph`](https://jonbogaty.com/radioactive-ralph/variants/savage-ralph/) | Maximum throughput | Budget is not the constraint | `--confirm-burn-budget` |
| [`/old-man-ralph`](https://jonbogaty.com/radioactive-ralph/variants/old-man-ralph/) | Imposed target state | Negotiation is over | `--confirm-no-mercy` |
| [`/world-breaker-ralph`](https://jonbogaty.com/radioactive-ralph/variants/world-breaker-ralph/) | Every agent on opus | The problem is genuinely catastrophic | `--confirm-burn-everything` |

See the full [variants index](https://jonbogaty.com/radioactive-ralph/variants/) for bios, arguments, and safety profiles.

## Install (once M2 ships)

Three paths, all shipping from one GoReleaser release:

```bash
# Homebrew (macOS, Linux via Linuxbrew, WSL2+Linuxbrew on Windows)
brew tap jbcom/tap
brew install ralph

# curl | sh (any POSIX environment)
curl -sSL https://jonbogaty.com/radioactive-ralph/install.sh | sh

# Claude Code plugin skill (bootstraps the binary on first run)
claude plugin marketplace add jbcom/radioactive-ralph
claude plugin install ralph@jbcom-plugins
```

For Windows natively (no WSL), `scoop install ralph` or `winget install jbcom.ralph` install the binary, but the supervisor itself runs only in POSIX environments; on Windows you'll use Ralph via WSL2.

## Commands (target CLI surface, post-M2)

```bash
ralph init                               # per-repo capability-matching wizard
ralph run --variant X [--detach]         # launch supervisor
ralph status [--variant X | --all]       # query via Unix socket
ralph attach --variant X                 # stream events
ralph stop [--variant X]                 # graceful shutdown
ralph doctor                             # environment health check
ralph service install --variant X        # emit launchd/systemd unit
ralph service list                       # show registered services
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
make build         # build ralph binary into ./dist/
```

## License

MIT. See [LICENSE](https://github.com/jbcom/radioactive-ralph/blob/main/LICENSE).
