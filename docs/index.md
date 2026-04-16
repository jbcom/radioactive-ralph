---
title: Home
lastUpdated: 2026-04-15
---

# radioactive-ralph

<div class="rr-hero">
  <div class="rr-hero__copy">
    <p class="rr-eyebrow">Binary-first runtime for repository-local AI work</p>
    <h2>radioactive-ralph</h2>
    <p class="rr-lead">A helpful little guy with many personalities: one binary, one plan DAG, one durable repo service, and one cockpit.</p>
    <div class="rr-actions">
      <a class="rr-button rr-button--primary" href="getting-started/">Get started</a>
      <a class="rr-button rr-button--secondary" href="variants/">Meet the Ralphs</a>
      <a class="rr-button rr-button--secondary" href="reference/">Reference</a>
    </div>
  </div>
  <div class="rr-hero__art">
    <img src="_static/ralph-mascot.png" alt="Radioactive Ralph mascot" />
  </div>
</div>

## Start here

<div class="rr-grid rr-grid--three">
  <a class="rr-card" href="getting-started/">
    <strong>Getting started</strong>
    <span>Install the binary, initialize a repo, let Fixit seed the plan DAG, and launch the repo service.</span>
  </a>
  <a class="rr-card" href="guides/">
    <strong>Guides</strong>
    <span>Learn how plans, Fixit, providers, safety floors, and the service/TUI flow fit together.</span>
  </a>
  <a class="rr-card" href="reference/">
    <strong>Reference</strong>
    <span>Read the current architecture, implementation state, testing stance, and generated Go API reference.</span>
  </a>
</div>

## What Ralph does

- Runs as a single Go binary: `radioactive_ralph`.
- Stores durable plan state in SQLite and executes it through a repo-local service.
- Exposes three operator surfaces: `service start`, `run --variant`, and `tui`.
- Defines Ralph personas in code instead of duplicating the canon in external sidecar specs.
- Treats the provider as a binding, not the product boundary.

```{toctree}
:hidden:

getting-started/index
guides/index
variants/index
reference/index
api/index
```
