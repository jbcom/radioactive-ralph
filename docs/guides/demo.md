---
title: Demo
lastUpdated: 2026-07-16
---

# Demo — Ralph in Action

The demo asset is intentionally treated like a release asset, not an embedded broken image. Once `assets/demo.gif` exists, this page becomes its permanent home in the docs.

## What the tape should show

1. `radioactive_ralph --help`
2. `radioactive_ralph doctor`
3. `radioactive_ralph service --help`
4. `radioactive_ralph --supervisor` (foreground, then Ctrl-C)
5. `radioactive_ralph` — the read-only TUI against a live supervisor

## Source of truth

- Tape file: `scripts/demo.tape`
- Recorder helper: `scripts/record-demo.sh`
- Output asset: `assets/demo.gif`

## Recording standard

The demo should feel like a poster for the project, not a tutorial. The goal is to prove three things quickly: Ralph is alive, Ralph is funny, and Ralph has a clear supervisor + client surface. Keep the tape deterministic and cheap to re-record; richer live-runtime footage can be a later release asset, not the baseline capture.
