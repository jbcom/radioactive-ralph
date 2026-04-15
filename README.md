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

radioactive-ralph is a binary-first orchestration tool for repo-local AI work.
It ships one executable, one durable plan store, and ten built-in Ralph
personas that change how the little guy thinks, behaves, and spends effort.

## Current direction

This branch pivots the product away from Claude marketplace/plugin packaging and
toward a single install story:

- install the `radioactive_ralph` binary
- run `radioactive_ralph init` in a repo
- let `init` register stdio MCP with Claude Code
- use the binary as the source of truth for variants, prompts, and runtime state

Current implementation is still Claude-CLI-backed, but the product contract is
now provider-oriented rather than plugin-oriented. The long-term goal is a
declarative provider layer where a repo can bind any supported agent CLI through
config, as long as the necessary prompt, model, effort, and structured-output
bindings are defined.

## What Ralph is

| Surface | What it does | Status |
|---|---|---|
| `radioactive_ralph` binary | Repo init, plan tooling, MCP serving, supervisor launch, doctor checks | Live |
| Claude Code MCP integration | Lets Claude talk to the binary over stdio MCP | Live |
| Built-in Ralph personas | Green, grey, red, blue, professor, fixit, immortal, savage, old-man, world-breaker | Live as in-code profiles |
| Provider abstraction | Declarative bindings for non-Claude agent CLIs | Target direction |

## Meet the Ralphs

| Variant | Specialty | Use it when | Gate |
|---|---|---|---|
| [`green-ralph`](https://jonbogaty.com/radioactive-ralph/variants/green-ralph/) | The classic loop | You want the default full-power orchestrator | — |
| [`grey-ralph`](https://jonbogaty.com/radioactive-ralph/variants/grey-ralph/) | Cheap mechanical cleanup | You need governance docs and boring hygiene fast | — |
| [`red-ralph`](https://jonbogaty.com/radioactive-ralph/variants/red-ralph/) | CI and PR fire drills | Something is on fire and you want one clean report | — |
| [`blue-ralph`](https://jonbogaty.com/radioactive-ralph/variants/blue-ralph/) | Read-only review | You want diagnosis without touching the code | — |
| [`professor-ralph`](https://jonbogaty.com/radioactive-ralph/variants/professor-ralph/) | Plan → execute → reflect | Strategy matters more than speed | — |
| [`fixit-ralph`](https://jonbogaty.com/radioactive-ralph/variants/fixit-ralph/) | Advisor + ROI-scored bursts | You need a free-form ask translated into a real durable plan | — |
| [`immortal-ralph`](https://jonbogaty.com/radioactive-ralph/variants/immortal-ralph/) | Recovery-first autonomy | You need it to survive the night | — |
| [`savage-ralph`](https://jonbogaty.com/radioactive-ralph/variants/savage-ralph/) | Maximum throughput | Budget is not the constraint | `--confirm-burn-budget` |
| [`old-man-ralph`](https://jonbogaty.com/radioactive-ralph/variants/old-man-ralph/) | Imposed target state | Negotiation is over | `--confirm-no-mercy` |
| [`world-breaker-ralph`](https://jonbogaty.com/radioactive-ralph/variants/world-breaker-ralph/) | Every agent on opus | The problem is genuinely catastrophic | `--confirm-burn-everything` |

See the full [variants index](https://jonbogaty.com/radioactive-ralph/variants/).

## Install

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

## Start a repo

```bash
radioactive_ralph init
radioactive_ralph run --variant fixit --advise \
  --topic stabilize-docs \
  --description "stabilize docs and line up the next implementation pass"
radioactive_ralph plan ls
radioactive_ralph run --variant green --foreground
```

`init` registers `radioactive_ralph` with Claude Code as a stdio MCP server
unless you pass `--skip-mcp`. `fixit --advise` now writes the repo-visible
advisor report and seeds the first durable DAG plan for that topic when no plan
with the same slug already exists for this repo.

## Current CLI surface

```bash
radioactive_ralph init
radioactive_ralph run --variant <name>
radioactive_ralph status --variant <name>
radioactive_ralph attach --variant <name>
radioactive_ralph stop --variant <name>
radioactive_ralph doctor
radioactive_ralph service install --variant <name>
radioactive_ralph service list
radioactive_ralph plan ls
radioactive_ralph plan show <id-or-slug>
radioactive_ralph plan next <id-or-slug>
radioactive_ralph plan import <path>
radioactive_ralph plan mark-done <id-or-slug> <task-id>
radioactive_ralph serve --mcp
radioactive_ralph mcp register
```

## Current provider reality

Today the runtime still shells out to `claude`, so these are the live operator
requirements:

- `claude` CLI installed and authenticated
- `gh` CLI installed and authenticated for GitHub workflows
- `git` available locally

The docs in [`docs/`](docs/) now describe Claude Code as one client of the
binary, not the identity of the product.

## Docs

- [Getting started](https://jonbogaty.com/radioactive-ralph/getting-started/)
- [Variants](https://jonbogaty.com/radioactive-ralph/variants/)
- [Architecture reference](https://jonbogaty.com/radioactive-ralph/reference/architecture/)
- [Claude MCP integration](https://jonbogaty.com/radioactive-ralph/guides/transports/)

## Contributing

See [AGENTS.md](/Users/jbogaty/src/jbcom/radioactive-ralph/AGENTS.md),
[STANDARDS.md](/Users/jbogaty/src/jbcom/radioactive-ralph/STANDARDS.md), and
[docs/reference/testing.md](/Users/jbogaty/src/jbcom/radioactive-ralph/docs/reference/testing.md).

```bash
git clone git@github.com:jbcom/radioactive-ralph.git
cd radioactive-ralph
make test
make lint
make build
```

## License

MIT. See [LICENSE](https://github.com/jbcom/radioactive-ralph/blob/main/LICENSE).
