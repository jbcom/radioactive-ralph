---
title: radioactive-ralph skill variants
updated: 2026-04-10
status: current
---

# Ralph has many forms

radioactive-ralph ships as a family of Claude Code skills, each one a different Ralph. They are genuinely behaviorally distinct — different model tiering, different parallelism limits, different tool allowlists, different termination conditions, different *personalities*. Pick the Ralph that fits the situation.

If you just want "the normal one," use [`/green-ralph`](./green-ralph/README.md). Everything else is a specialization.

## The variants

| Variant | One-liner | When to use | Gate |
|---|---|---|---|
| [`/green-ralph`](./green-ralph/README.md) | The Classic. Unlimited loop, all repos, sensible model tiering. | Default autonomous mode. The one you run when there's no specific crisis. | — |
| [`/grey-ralph`](./grey-ralph/README.md) | The First Form. Single repo, haiku only, file hygiene only. | Frontmatter, CHANGELOG, governance stubs. Cheap janitor passes. | — |
| [`/red-ralph`](./red-ralph/README.md) | The Principal. Single cycle, CI failures and PR blockers only, structured report. | Something is on fire and you need a clean report at the end. | — |
| [`/blue-ralph`](./blue-ralph/README.md) | The Observer. Read-only review — `Write`/`Edit` structurally excluded. | Second-opinion review across all repos without any risk of modification. | — |
| [`/professor-ralph`](./professor-ralph/README.md) | The Integrated. Opus planning → sonnet execution → sonnet reflection. | Strategic work. When you want the orchestrator to *think* before it acts. | — |
| [`/joe-fixit-ralph`](./joe-fixit-ralph/README.md) | The Fixer. N cycles, single repo, highest-ROI task per cycle, "bill" at the end. | Budget-conscious sessions. Predictable stopping condition. Small reviewable PRs. | — |
| [`/immortal-ralph`](./immortal-ralph/README.md) | The One Who Comes Back. Crash-resistant, retries everything, state persisted obsessively, sonnet only. | Overnight/weekend runs. Flaky network. "Set and forget" reliability. | — |
| [`/savage-ralph`](./savage-ralph/README.md) | The Mindless. 10 parallel agents, model escalation (+1 tier), zero sleep. | Clearing a large backlog fast when budget is not the constraint. | `--confirm-burn-budget` |
| [`/old-man-ralph`](./old-man-ralph/README.md) | The Maestro. Totalitarian precision. Force-resets branches, resolves conflicts `-X ours`, deletes blockers. | When you have a clear target state and want it *imposed*, not negotiated. | `--confirm-no-mercy` |
| [`/world-breaker-ralph`](./world-breaker-ralph/README.md) | The World Breaker. Every agent opus. All repos. Zero sleep. | Critical incident. Major architecture propagation. You have lost something and nothing else can be wrong. | `--confirm-burn-everything` |

## How the variants differ at a glance

| | parallelism | default model | sleep | scope | cycle limit |
|---|---|---|---|---|---|
| green-ralph | 6 | mixed (haiku/sonnet/opus) | 30s | configured repos | unlimited |
| grey-ralph | 1 | haiku | — | cwd only | 1 (single sweep) |
| red-ralph | 8 | sonnet (opus escalation) | — | configured repos | 1 (single cycle) |
| blue-ralph | 4 | sonnet | 10m | configured repos | unlimited (default: single-pass) |
| professor-ralph | 4 | opus → sonnet → sonnet | 5m | configured repos | unlimited |
| joe-fixit-ralph | 1 | haiku/sonnet | — | single repo | N (default 3) |
| immortal-ralph | 3 | sonnet | 2m | configured repos | unlimited |
| savage-ralph | 10 | escalated +1 tier | 0s | configured + discovered | unlimited |
| old-man-ralph | 3 | sonnet (opus for planning) | — | single branch target | 1 (single imposition) |
| world-breaker-ralph | 10 | opus | 0s | all repos, all orgs | unlimited |

## Installing

All variants ship in the same package. Install them individually or all at once:

```bash
# Install one
ralph install-skill --variant green-ralph

# Install all
ralph install-skill --all
```

Or install radioactive-ralph as a Claude Code plugin — see the [main README](../README.md#install-as-a-claude-code-plugin).

## Picking a variant

A rough decision tree:

- **"I just want it running"** → `green-ralph`
- **"I have 20 minutes and a budget"** → `joe-fixit-ralph --cycles 3`
- **"CI is on fire"** → `red-ralph`
- **"Review this before you touch anything"** → `blue-ralph`
- **"Think before you act"** → `professor-ralph`
- **"Frontmatter, CHANGELOGs, boring stuff"** → `grey-ralph`
- **"Overnight, survive anything"** → `immortal-ralph`
- **"I need 10x throughput and I can pay"** → `savage-ralph --confirm-burn-budget`
- **"Make this branch match my vision, burn the rest"** → `old-man-ralph --confirm-no-mercy`
- **"Something is genuinely on fire and normal isn't enough"** → `world-breaker-ralph --confirm-burn-everything`

## Why so many?

Because autonomy isn't one thing. "Run the loop forever" and "fix CI and exit" are different problems with different safety profiles, different budget shapes, and different ideal tool allowlists. Instead of one giant skill with fifteen flags, radioactive-ralph ships a family of small, opinionated variants — each one genuinely good at one thing, each one structurally constrained to stay in its lane.

Also: there is a Ralph for every situation, because Ralph contains multitudes, and the multitudes all have opinions.
