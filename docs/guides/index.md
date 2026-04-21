---
title: Guides
lastUpdated: 2026-04-15
---

# Guides

| Guide | What it covers |
|---|---|
| [Runtime surfaces](./transports.md) | How `service`, attached `run`, and `tui` fit together |
| [TUI cockpit](./tui.md) | Keyboard shortcuts, queue views, and operator actions in the socket-backed cockpit |
| [Plan format](./plan-format.md) | How repo-visible plan markdown and the live DAG store relate, plus what `plan import` accepts |
| [Fixit delegation](./fixit-delegation.md) | How fixit advisor mode writes recommendation docs and seeds the durable plan DAG |
| [Cassette VCR](./cassette-vcr.md) | Deterministic replay for the Claude provider backend tests |
| [Safety floors](./safety-floors.md) | Non-negotiable guardrails for risky variants and service contexts |
| [Design](./design.md) | Product vision, persona philosophy, and the binary-first direction |
| [Fixit pipeline design](../design/fixit-plan-pipeline.md) | The deeper design note for fixit's staged planning pipeline |
| [Demo](./demo.md) | How the recorded terminal demo is structured and how to re-record it |
| [Launch](./launch.md) | Launch-day asset, packaging, verification, and copy checklist |

```{toctree}
:hidden:

transports
tui
plan-format
fixit-delegation
cassette-vcr
safety-floors
design
launch
demo
```
