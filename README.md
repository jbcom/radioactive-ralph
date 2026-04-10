---
title: radioactive-ralph
updated: 2026-04-10
status: current
---

# radioactive-ralph

<p align="center">
  <img src="assets/ralph-mascot.png" alt="Autonomous continuous development orchestrator for Claude Code." width="400"/>
</p>

<p align="center">
  <em>Autonomous continuous development orchestrator for Claude Code.</em>
</p>

<p align="center">
  <a href="https://pypi.org/project/radioactive-ralph/"><img src="https://img.shields.io/pypi/v/radioactive-ralph" alt="PyPI"/></a>
  <a href="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml"><img src="https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml/badge.svg" alt="CI"/></a>
  <a href="https://jbcom.github.io/radioactive-ralph/"><img src="https://img.shields.io/badge/docs-github%20pages-blue" alt="Docs"/></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT License"/></a>
</p>

## What it is

radioactive-ralph drives Claude Code across a portfolio of git repos — continuously, without human intervention. It ships in **two modes**:

1. **As a Claude Code plugin** — a family of ten `/ralph` variants you run directly inside a Claude Code session. No separate daemon, no API key setup, no subprocess orchestration.
2. **As a standalone Python daemon** — `pip install radioactive-ralph` and run `ralph run` to get a persistent loop that survives context resets, spawns `claude --print` subprocesses, and lives *outside* any Claude Code session.

**The only time you should be engaged is to brainstorm the vision.**

## Ralph has many forms

radioactive-ralph is not one skill. It's a family of ten, each a genuinely distinct operating mode with its own model tiering, parallelism, tool allowlist, and termination condition. See [`skills/`](./skills/README.md) for the full variants index.

| Variant | One-liner | Gate |
|---|---|---|
| [`/green-ralph`](./skills/green-ralph/README.md) | The Classic. Unlimited loop, all repos, sensible model tiering. | — |
| [`/grey-ralph`](./skills/grey-ralph/README.md) | The First Form. Single repo, haiku only, file hygiene. | — |
| [`/red-ralph`](./skills/red-ralph/README.md) | The Principal. Single cycle, CI failures + PR blockers, structured report. | — |
| [`/blue-ralph`](./skills/blue-ralph/README.md) | The Observer. Read-only — `Write`/`Edit` structurally excluded. | — |
| [`/professor-ralph`](./skills/professor-ralph/README.md) | The Integrated. Opus plan → sonnet execute → sonnet reflect. | — |
| [`/joe-fixit-ralph`](./skills/joe-fixit-ralph/README.md) | The Fixer. N cycles, single repo, ROI-scored, prints a bill. | — |
| [`/immortal-ralph`](./skills/immortal-ralph/README.md) | The One Who Comes Back. Crash-resistant. Obsessive state persistence. | — |
| [`/savage-ralph`](./skills/savage-ralph/README.md) | The Mindless. 10 parallel, +1 tier escalation, zero sleep. | `--confirm-burn-budget` |
| [`/old-man-ralph`](./skills/old-man-ralph/README.md) | The Maestro. Force-resets branches, resolves conflicts `-X ours`. | `--confirm-no-mercy` |
| [`/world-breaker-ralph`](./skills/world-breaker-ralph/README.md) | The World Breaker. Every agent opus. All repos. Zero sleep. | `--confirm-burn-everything` |

## Install as a Claude Code plugin

```bash
# Add this repo as a marketplace
claude plugins marketplace add github:jbcom/radioactive-ralph

# Install
claude plugin install radioactive-ralph@radioactive-ralph

# Then, inside Claude Code:
/green-ralph
```

All ten variants are installed at once. Use whichever you need per-session.

## Install as a standalone daemon

```bash
# Run instantly — no install required
uvx radioactive-ralph run

# Or install permanently
pip install radioactive-ralph
ralph run
```

## How the daemon mode works

```
┌─────────────────────────────────────────────────────┐
│                  radioactive-ralph                   │
│                   (Python daemon)                    │
│                                                      │
│  scan PRs → merge ready → review → address feedback  │
│       → discover work → spawn agents → loop          │
│                                                      │
│  State: ~/.radioactive-ralph/state.json              │
└──────────────┬──────────────────────────────────────┘
               │ spawns
               ▼
┌─────────────────────────────────────────────────────┐
│            claude CLI subprocesses                   │
│        (one per repo, run in parallel)               │
│                                                      │
│  Each agent: reads context, does work, opens PR      │
└─────────────────────────────────────────────────────┘
```

## Model tiering

| Task | Model |
|------|-------|
| Doc sweeps, frontmatter, bulk cleanup | `claude-haiku-4-5` |
| Feature work, bug fixes, PR review | `claude-sonnet-4-6` (default) |
| Architecture, security, vision | `claude-opus-4-6` |

Each variant has its own model tiering policy — see the individual skill READMEs.

## Configuration

Create `~/.radioactive-ralph/config.toml`:

```toml
[orgs]
arcade-cabinet = "~/src/arcade-cabinet"
jbcom = "~/src/jbcom"

bulk_model = "claude-haiku-4-5-20251001"
default_model = "claude-sonnet-4-6"
deep_model = "claude-opus-4-6"
max_parallel_agents = 5
```

Set `ANTHROPIC_API_KEY` in your environment (daemon mode only — the plugin mode uses your existing Claude Code session).

## Commands (daemon mode)

```bash
ralph run               # Start the daemon
ralph dashboard         # Open the live Rich terminal dashboard (reads state)
ralph status            # Show current state
ralph discover          # Show discovered work items
ralph pr list           # List all open PRs with classification
ralph pr merge          # Merge all MERGE_READY PRs
ralph doctor            # Run diagnostic health checks on the environment
ralph doctor --json     # Same checks, JSON output for CI/scripting
ralph stop              # Stop the running daemon
ralph install-skill     # Install a skill variant into ~/.claude/skills/
```

The `dashboard` command is read-only — it tails `~/.radioactive-ralph/state.json`
every second and renders a live multi-panel view (PRs, work queue, active agents,
recent Ralph events, and stats footer). Run it in a second terminal alongside
`ralph run`. Theme it with any variant: `ralph dashboard -v old-man-ralph`.

## Personality

radioactive-ralph logs in the voice of Ralph Wiggum. Every variant has its own color scheme and quote set. The messages are variant-aware, event-aware, and randomly selected — so Ralph stays surprising while you watch him do 400 cycles. Yes, really.

```
[green-ralph] I dressed myself! And now I'm running!
[green-ralph] Cycle 1! I'm doing cycle 1! That's where I'm a developer!
[green-ralph] Scanning 12 repos for pull requests. I can see them with my eyes!
[green-ralph] Found 18 open pull requests across 12 repos! I found them!
[green-ralph] Merging PR #142 in radioactive-ralph. Squash! Like the game!
[green-ralph] PR #142 merged! Everybody's hugging!
[green-ralph] Sleeping for 30 seconds. Oh boy, sleep! That's where I'm a Viking!
```

See [`src/radioactive_ralph/ralph_says.py`](./src/radioactive_ralph/ralph_says.py) for the full personality module.

## Requirements

- Python 3.12+
- `claude` CLI installed and authenticated (`claude login`)
- `gh` CLI installed and authenticated (`gh auth login`)
- `ANTHROPIC_API_KEY` set in environment (daemon mode only)

## Contributing

See [AGENTS.md](AGENTS.md) for agentic operating protocols, [STANDARDS.md](STANDARDS.md) for code quality rules, and [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow.

```bash
git clone git@github.com:jbcom/radioactive-ralph.git
cd radioactive-ralph
uv sync --all-extras
uv run pytest
```

## License

MIT. See [LICENSE](./LICENSE).

<details>
<summary><strong>Launch notes</strong> (maintainers — uploading the social preview + demo assets)</summary>

### Social preview image

GitHub shows a custom image when someone shares this repo on LinkedIn, HN, Slack, X, etc. The image file lives at [`assets/social-preview.png`](./assets/social-preview.png) (1280x640 PNG). To upload or replace it:

1. Generate or update `assets/social-preview.png` per the spec in [`assets/ASSETS.md`](./assets/ASSETS.md#2-social-preview--assetssocial-previewpng)
2. Commit and push the file (so it's versioned and reviewable)
3. Upload it to GitHub:
   - Go to **Settings -> General -> Social preview**
   - Click **Edit** -> **Upload an image...**
   - Select the PNG from your local checkout (`assets/social-preview.png`)
   - Save. GitHub caches the new card; force-refresh via [opengraph.dev](https://www.opengraph.dev) or LinkedIn's [Post Inspector](https://www.linkedin.com/post-inspector/) if the old one sticks

### Demo GIF

The README can embed [`assets/demo.gif`](./assets/demo.gif) once it's recorded. To record (or re-record) it:

```bash
# Install vhs once
brew install vhs    # or: go install github.com/charmbracelet/vhs@latest

# Record — the tape file is the source of truth
./scripts/record-demo.sh
```

Edit [`scripts/demo.tape`](./scripts/demo.tape) to change what the demo shows. The recording is fully deterministic — same tape + same ralph state ≈ same GIF.

### Full asset checklist

See [`docs/LAUNCH.md`](./docs/LAUNCH.md) for the full launch checklist and the LinkedIn / Hacker News draft copy.

</details>
