---
title: Home
lastUpdated: 2026-04-15
---

# radioactive-ralph

<div class="rr-hero">
  <div class="rr-hero__copy">
    <p class="rr-eyebrow">Binary-first orchestration for repository-local AI work</p>
    <h2>radioactive-ralph</h2>
    <p class="rr-lead">A helpful little guy with many personalities: one binary, one plan store, one MCP surface, and a lot of Ralphspeaking.</p>
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
    <span>Install the binary, initialize a repo, register Claude MCP, and launch your first Ralph persona.</span>
  </a>
  <a class="rr-card" href="guides/">
    <strong>Guides</strong>
    <span>Learn how plans, fixit, safety floors, Claude MCP, cassettes, and launch checks fit together.</span>
  </a>
  <a class="rr-card" href="reference/">
    <strong>Reference</strong>
    <span>Read the current architecture, implementation state, testing stance, and generated Go API reference.</span>
  </a>
</div>

## What Ralph does

- Runs as a single Go binary: `radioactive_ralph`.
- Treats Claude Code as a client over stdio MCP, not as the product boundary.
- Defines Ralph personas in code instead of outsourcing the canon to marketplace skills.
- Keeps repo-root docs in `docs/`.
- Leaves room for future provider bindings beyond `claude`.

```{toctree}
:hidden:

getting-started/index
guides/index
variants/index
reference/index
api/index
```
