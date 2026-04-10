---
title: Design
updated: 2026-04-10
status: current
domain: product
---

# Design — radioactive-ralph

## Vision

AI-driven development should be continuous and autonomous, not session-based.

Today: open Claude, give it a task, wait for it to finish, tell it what to do next.
Tomorrow: open Claude (or don't), and it's already working.

**radioactive-ralph is the bridge.**

## What it IS

- An external Python daemon that orchestrates Claude Code across many repos
- A persistent brain that survives context resets, PR merges, and rate limits
- A work discovery engine that always finds the next valuable thing to do
- A model efficiency layer — haiku for bulk, sonnet for features, opus for vision
- An internal code reviewer — no external dependency on CodeRabbit

## What it IS NOT

- A replacement for human judgment on vision and direction
- A way to merge unreviewed, untested code
- A black box — all state is readable JSON, all actions are auditable git history
- A vendor lock-in — uses `claude` CLI and `gh` CLI, both open

## Core principles

**Never halt.** There is always more to do — missing docs, open PRs, STATE.md items, features to build. If the queue is empty, run discovery again.

**Human engagement only for vision.** Architecture decisions, product direction, new initiatives — that's the human's domain. Everything else: autonomous.

**Cost efficiency.** Haiku handles 80% of the work at 10% of the cost. Opus for < 5% of tasks where it genuinely matters.

**External persistence.** Context windows reset. Daemons don't. The daemon is the source of truth.

**Auditable.** Every action creates a git commit or a PR. Nothing happens in the dark.

## User experience

```bash
# Zero-install, instant start
uvx radioactive-ralph run

# Walk away. Come back to:
gh pr list  # PRs open across all your repos
```

## Non-goals

- GUI or web dashboard (gh CLI + terminal is sufficient)
- Multi-user / team coordination (single-operator tool)
- Replacing CI/CD (augments it, doesn't replace it)
