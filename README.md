---
title: radioactive-ralph
updated: 2026-04-10
status: current
---

# radioactive-ralph

<p align="center">
  <img src="assets/ralph-mascot.png" alt="radioactive-ralph — I'M HELPING!" width="400"/>
</p>

> **"I'M HELPING!"** — Autonomous continuous development orchestrator for Claude Code.

[![PyPI](https://img.shields.io/pypi/v/radioactive-ralph)](https://pypi.org/project/radioactive-ralph/)
[![CI](https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml/badge.svg)](https://github.com/jbcom/radioactive-ralph/actions/workflows/ci.yml)
[![Docs](https://img.shields.io/badge/docs-github%20pages-blue)](https://jbcom.github.io/radioactive-ralph/)

## What it is

radioactive-ralph is an external persistent daemon that drives Claude Code across a portfolio of GitHub repos — 24/7, without human intervention. It survives context resets, PR merges, process restarts, and API rate limits because it lives *outside* Claude's context window.

**The only time you should be engaged is to brainstorm the vision.**

## Two ways to run

radioactive-ralph supports two execution modes that share the same logic and state file.

### Mode A — Claude Code skill (no API key needed)

Runs _inside_ an active Claude Code session. Uses the session's Agent tool to spawn subagents — no separate API key or background process required.

```bash
# Install once
pip install radioactive-ralph
ralph install-skill

# Then in any Claude Code session:
/radioactive-ralph
```

### Mode B — Standalone daemon

Runs as an independent background process. Survives session end, context resets, and machine restarts. Requires `ANTHROPIC_API_KEY`.

```bash
# Run instantly — no install required
uvx radioactive-ralph run

# Or install permanently and run as a daemon
pip install radioactive-ralph
ralph run
```

Both modes read the same `~/.radioactive-ralph/config.toml` and write to the same `~/.radioactive-ralph/state.json`.

## How it works

```
┌─────────────────────────────────────────────────────────────────┐
│                      radioactive-ralph                           │
│                                                                  │
│  scan PRs → merge ready → review → discover work → execute loop │
│                                                                  │
│  State: ~/.radioactive-ralph/state.json                          │
└──────────────┬──────────────────────────┬───────────────────────┘
               │ Mode A (skill)           │ Mode B (daemon)
               ▼                          ▼
   Agent tool subagents           claude CLI subprocesses
   (within Claude Code)           (independent processes)
```

## Model tiering

| Task | Model |
|------|-------|
| Doc sweeps, frontmatter, bulk cleanup | `claude-haiku-4-5` |
| Feature work, bug fixes, PR review | `claude-sonnet-4-6` (default) |
| Architecture, security, vision | `claude-opus-4-6` |

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

Set `ANTHROPIC_API_KEY` in your environment.

## Commands

```bash
ralph install-skill                       # Install both /ralph and /radioactive-ralph skills
ralph install-skill --variant ralph       # Install lightweight single-repo /ralph skill only
ralph install-skill --variant radioactive-ralph  # Install multi-repo /radioactive-ralph only
ralph run            # Start the standalone daemon
ralph status         # Show current state (works for both modes)
ralph discover       # Show discovered work items
ralph pr list        # List all open PRs with classification
ralph pr merge       # Merge all MERGE_READY PRs
ralph stop           # Stop the running daemon
```

## Requirements

- Python 3.12+
- `gh` CLI installed and authenticated (`gh auth login`)

**Mode A (skill) additionally requires:**
- Claude Code installed (`npm install -g @anthropic-ai/claude-code`)

**Mode B (daemon) additionally requires:**
- `ANTHROPIC_API_KEY` set in environment
- `claude` CLI installed (`pip install claude-cli` or `npm install -g @anthropic-ai/claude-code`)

## Contributing

See [AGENTS.md](AGENTS.md) for agentic operating protocols and [STANDARDS.md](STANDARDS.md) for code quality rules.

```bash
git clone git@github.com:jbcom/radioactive-ralph.git
cd radioactive-ralph
uv sync --all-extras
uv run pytest
```
