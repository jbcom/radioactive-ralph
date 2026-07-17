---
title: Home
lastUpdated: 2026-07-16
---

# radioactive-ralph

<div class="rr-hero">
  <div class="rr-hero__copy">
    <p class="rr-eyebrow">A supervised-execution runtime for local AI-agent CLIs</p>
    <h2>radioactive-ralph</h2>
    <p class="rr-lead">One binary, two modes: a supervisor that owns every agent's pty and the one durable database, and a dumb client that renders a read-only view of it.</p>
    <div class="rr-actions">
      <a class="rr-button rr-button--primary" href="getting-started/">Get started</a>
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
    <span>Install the binary, start the supervisor, register a project, and run the client.</span>
  </a>
  <a class="rr-card" href="guides/">
    <strong>Guides</strong>
    <span>Learn how the supervisor, client, plans, and providers fit together.</span>
  </a>
  <a class="rr-card" href="reference/">
    <strong>Reference</strong>
    <span>Read the current architecture, implementation state, testing stance, and generated Go API reference.</span>
  </a>
</div>

## What Ralph does

- Runs as a single Go binary: `radioactive_ralph`.
- The supervisor (`--supervisor`) owns every agent's pty directly and
  never lets one block the system.
- Stores durable plan, project, and spend state in one user-level SQLite
  database — never a per-repo file.
- The client (no flag) discovers the supervisor and renders a read-only
  macro/meso/micro TUI.
- Completion is orchestrator-verified against acceptance criteria, never
  a worker's self-report.
- Treats the provider as a binding, not the product boundary.

```{toctree}
:hidden:

getting-started/index
guides/index
runbooks/index
launch/index
design/index
reference/index
api/index
```
