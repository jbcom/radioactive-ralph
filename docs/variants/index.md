---
title: Variants
lastUpdated: 2026-04-15
---

# Ralph has many forms

radioactive-ralph ships ten built-in personas. Each one is a separate
operating mode with its own safety profile, budget shape, voice, and tool
posture. Pick a persona with `radioactive_ralph run --variant <name>`.

| Variant | Specialty | Best when | Declared gate |
|---|---|---|---|
| [`green-ralph`](./green-ralph.md) | The classic always-on loop | You want the default orchestrator behavior | — |
| [`grey-ralph`](./grey-ralph.md) | Cheap mechanical hygiene | You need frontmatter, CHANGELOG, and governance cleanup | — |
| [`red-ralph`](./red-ralph.md) | Single-pass incident response | CI or PR blockers are on fire | — |
| [`blue-ralph`](./blue-ralph.md) | Read-only review | You want diagnosis without modification | — |
| [`professor-ralph`](./professor-ralph.md) | Plan, execute, reflect | Strategy matters more than speed | — |
| [`fixit-ralph`](./fixit-ralph.md) | Advisor + ROI-scored bursts | You need a variant recommendation OR fixed-effort small PRs | — |
| [`immortal-ralph`](./immortal-ralph.md) | Recovery-first autonomy | You want it to survive the night | — |
| [`savage-ralph`](./savage-ralph.md) | Throughput at any cost | Budget is not the constraint | `--confirm-burn-budget` |
| [`old-man-ralph`](./old-man-ralph.md) | Imposed target state | Negotiation is over | `--confirm-no-mercy` |
| [`world-breaker-ralph`](./world-breaker-ralph.md) | Every agent on opus | The problem is genuinely catastrophic | `--confirm-burn-everything` |

```{toctree}
:hidden:

green-ralph
grey-ralph
red-ralph
blue-ralph
professor-ralph
fixit-ralph
immortal-ralph
savage-ralph
old-man-ralph
world-breaker-ralph
```
