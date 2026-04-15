---
title: Home
lastUpdated: 2026-04-15
---

# radioactive-ralph

<div class="rr-hero">
  <div class="rr-hero__copy">
    <p class="rr-eyebrow">Autonomous development orchestration for Claude Code</p>
    <h2>radioactive-ralph</h2>
    <p class="rr-lead">One Go binary. Ten Ralph variants. Repo-root docs. A persistent little guy with a proper plan store and a real docs theme again.</p>
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
    <span>Install the binary, initialize a repo, register MCP, and launch your first variant.</span>
  </a>
  <a class="rr-card" href="guides/">
    <strong>Guides</strong>
    <span>Transport modes, plans, fixit delegation, safety floors, demo flow, and release checklists.</span>
  </a>
  <a class="rr-card" href="reference/">
    <strong>Reference</strong>
    <span>Current architecture, implementation state, testing, and generated Go API reference.</span>
  </a>
</div>

## What Ralph does

- Runs per-repo supervisors from the `radioactive_ralph` Go binary.
- Keeps the canonical documentation tree in `docs/`, not buried under a site subproject.
- Uses generated Go API markdown from `gomarkdoc` instead of Sphinx AutoAPI.
- Keeps the original Shibuya-based visual system: acid green palette, gradients, card panels, and the Ralph fonts.

```{toctree}
:hidden:

getting-started/index
guides/index
variants/index
reference/index
api/index
```
