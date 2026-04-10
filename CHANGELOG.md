---
title: CHANGELOG
updated: 2026-04-10
status: current
---

# Changelog

All notable changes to this project will be documented in this file.
Format: [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Initial Python package structure with hatchling build
- `Orchestrator` async daemon loop with 8-phase cycle
- `PRManager` — gh CLI wrapper for PR classification and merge
- `Reviewer` — internal code review via Anthropic API (haiku/sonnet tiering)
- `WorkDiscovery` — scans repos for missing docs, reads STATE.md and DESIGN.md
- `AgentRunner` — spawns claude CLI subprocesses with model selection
- `State` — durable JSON persistence with dedup and pruning
- `AutoloopConfig` — TOML-based config with sensible defaults
- Click CLI: `ralph run`, `ralph status`, `ralph discover`, `ralph pr list/merge`, `ralph stop`
- `uvx radioactive-ralph` support for zero-install execution
- Sphinx documentation with RTD theme, published to GitHub Pages
- CI/CD: GitHub Actions with OIDC PyPI publishing and Sphinx Pages deploy
- release-please for automated changelog and versioning
- dependabot for weekly dependency updates

[Unreleased]: https://github.com/jbcom/radioactive-ralph/compare/HEAD...HEAD
