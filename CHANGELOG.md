---
title: CHANGELOG
updated: 2026-04-10
status: current
---

# Changelog

All notable changes to this project will be documented in this file.
Format: [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), [Semantic Versioning](https://semver.org/).

## [0.5.0](https://github.com/jbcom/radioactive-ralph/compare/v0.4.0...v0.5.0) (2026-04-10)


### Features

* add automerge workflow and clean up CD ([#20](https://github.com/jbcom/radioactive-ralph/issues/20)) ([06bf01f](https://github.com/jbcom/radioactive-ralph/commit/06bf01ff277f27985eb751c5c73aed093db7824b))
* automate release asset generation ([#16](https://github.com/jbcom/radioactive-ralph/issues/16)) ([7474891](https://github.com/jbcom/radioactive-ralph/commit/74748912a30c59b4c6e5d7da3a5f51bdbde4abd3))


### Bug Fixes

* GitHub native PR comment for demo GIF ([#19](https://github.com/jbcom/radioactive-ralph/issues/19)) ([ba1fd89](https://github.com/jbcom/radioactive-ralph/commit/ba1fd89399b90c23e7b375d9e6d7239f6d0233e4))

## [0.4.0](https://github.com/jbcom/radioactive-ralph/compare/v0.3.0...v0.4.0) (2026-04-10)


### Features

* modernize documentation (Shibuya + AutoAPI + Fuzzy Bubbles) ([#12](https://github.com/jbcom/radioactive-ralph/issues/12)) ([2537f08](https://github.com/jbcom/radioactive-ralph/commit/2537f081343f3ca0900d77fb9733140b757afd5d))

## [0.3.0](https://github.com/jbcom/radioactive-ralph/compare/v0.2.0...v0.3.0) (2026-04-10)


### Features

* integrate SonarQube and test reporting ([#11](https://github.com/jbcom/radioactive-ralph/issues/11)) ([236b7a7](https://github.com/jbcom/radioactive-ralph/commit/236b7a7c82ea33a47cc96c4855d65006a1c55882))

## [0.2.0](https://github.com/jbcom/radioactive-ralph/compare/v0.1.0...v0.2.0) (2026-04-10)


### Features

* 100% test coverage and modernized CI/CD workflows ([#9](https://github.com/jbcom/radioactive-ralph/issues/9)) ([9f79067](https://github.com/jbcom/radioactive-ralph/commit/9f790671fc077be18b98bb9d3d4c7f5f832cc9c9))

## 0.1.0 (2026-04-10)


### Features

* forge abstraction layer + GitPython local git ops ([#3](https://github.com/jbcom/radioactive-ralph/issues/3)) ([9bcb26f](https://github.com/jbcom/radioactive-ralph/commit/9bcb26f86229c9afca26f2e460564643105f9c2a))
* initial radioactive-ralph v0.1.0 release ([b0af5c6](https://github.com/jbcom/radioactive-ralph/commit/b0af5c65b2aba3de7fae80194500081bf0c7e92c))

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
