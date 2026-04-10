<h1 align="center">radioactive-ralph</h1>

<p align="center">
  <img src="https://raw.githubusercontent.com/jbcom/radioactive-ralph/main/assets/brand/ralph-mascot.png" alt="Autonomous continuous development orchestrator for Claude Code." width="400"/>
</p>

<p align="center">
  <em>Autonomous continuous development orchestrator for Claude Code.</em>
</p>

<p align="center">
  <a href="https://pypi.org/project/radioactive-ralph/"><img src="https://img.shields.io/pypi/v/radioactive-ralph" alt="PyPI"/></a>
  <a href="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml"><img src="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <a href="https://sonarcloud.io/summary/new_code?id=jbcom_radioactive-ralph"><img src="https://sonarcloud.io/api/project_badges/measure?project=jbcom_radioactive-ralph&metric=vulnerabilities" alt="Vulnerabilities"/></a>
  <a href="https://jonbogaty.com/radioactive-ralph/"><img src="https://img.shields.io/badge/docs-jonbogaty.com%2Fradioactive--ralph-22c55e" alt="Docs"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"/></a>
</p>

radioactive-ralph drives Claude Code across a portfolio of git repos — continuously, with a sense of humor, and with enough structure to keep the funny little guy from burning the school down.

## What it is

| Mode | What you get | Best for |
|---|---|---|
| Claude Code plugin | Ten Ralph variants installed into Claude Code as a marketplace plugin | In-session autonomy with explicit tool/safety boundaries |
| Python daemon | `ralph run` loop that survives context resets and spawns `claude --print` subprocesses | Long-running orchestration outside any single Claude session |

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

## Install as a Claude Code plugin

```bash
claude plugins marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive-ralph@radioactive-ralph

# inside Claude Code
/green-ralph
```

## Install as a standalone daemon

```bash
uvx radioactive-ralph run

# or install permanently
pip install radioactive-ralph
ralph run
```

## Core commands

```bash
ralph run
ralph dashboard
ralph status
ralph discover
ralph pr list
ralph pr merge
ralph install-skill --all
```

## Docs and design system

- [Getting started](https://jonbogaty.com/radioactive-ralph/getting-started/)
- [Ralph variants](https://jonbogaty.com/radioactive-ralph/variants/)
- [Architecture reference](https://jonbogaty.com/radioactive-ralph/reference/architecture/)
- [API reference](https://jonbogaty.com/radioactive-ralph/autoapi/)
- [Launch guide](https://jonbogaty.com/radioactive-ralph/guides/launch/)

## Requirements

- Python 3.12+
- `claude` CLI installed and authenticated (`claude login`)
- `gh` CLI installed and authenticated (`gh auth login`)
- `ANTHROPIC_API_KEY` set in the environment for daemon mode only

## Contributing

See [AGENTS.md](./AGENTS.md), [STANDARDS.md](./STANDARDS.md), and [CONTRIBUTING guidance in the docs](https://jonbogaty.com/radioactive-ralph/reference/testing/).

```bash
git clone git@github.com:jbcom/radioactive-ralph.git
cd radioactive-ralph
python3 -m pip install --user hatch
python3 -m hatch test
```

## License

MIT. See [LICENSE](https://github.com/jbcom/radioactive-ralph/blob/main/LICENSE).
