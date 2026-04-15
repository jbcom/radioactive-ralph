---
title: Getting started
updated: 2026-04-10
status: current
domain: product
---

# Get started

## Pick your delivery vehicle

| Mode | Best for | Install |
|---|---|---|
| Claude Code plugin | In-session autonomy with ten curated Ralph variants | `claude plugins marketplace add github:jbcom/radioactive-ralph` then `claude plugin install radioactive-ralph@radioactive-ralph` |
| Python daemon | Persistent orchestration outside Claude sessions | `uvx radioactive-ralph run` or `pip install radioactive-ralph && ralph run` |

## Quickstart

### Claude Code plugin

```bash
claude plugins marketplace add github:jbcom/radioactive-ralph
claude plugin install radioactive-ralph@radioactive-ralph

# inside Claude Code
/green-ralph
```

### Python daemon

```bash
uvx radioactive-ralph run

# or
pip install radioactive-ralph
ralph run
```

## Core commands

| Command | What it does |
|---|---|
| `radioactive_ralph run` | Start the persistent orchestrator loop |
| `ralph dashboard` | Open the live Rich dashboard |
| `radioactive_ralph status` | Show current state and recent Ralph events |
| `ralph discover` | Show the current queue of discovered work |
| `ralph pr list` | List and classify pull requests |
| `ralph install-skill --all` | Install every Ralph variant locally |

## Requirements

- Python 3.12+
- `claude` CLI installed and authenticated
- `gh` CLI installed and authenticated
- `ANTHROPIC_API_KEY` for daemon mode only
