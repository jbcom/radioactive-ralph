---
title: CHANGELOG
updated: 2026-04-14
status: current
---

# Changelog

All notable changes to this project will be documented in this file.
Format: [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- **Architectural pivot** — the daemon is being rewritten into a per-repo
  meta-orchestrator that owns managed `claude -p` subprocesses via stream-json
  stdin/stdout. Rationale and full plan in
  [`docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`](docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md).
- `.claude-plugin/marketplace.json` — marketplace renamed to `jbcom-plugins`,
  plugin renamed to `ralph`, `strict: false`, skills listed explicitly. The
  previous name collision (`radioactive-ralph@radioactive-ralph`) made the
  install invocation ambiguous; the new invocation is
  `claude plugin install ralph@jbcom-plugins`.
- README install + command documentation corrected to match the real CLI.
  The four phantom commands (`dashboard`, `discover`, `pr list/merge`,
  `install-skill`) and the `claude --print subprocesses` fiction are gone.
- Auth helpers moved from `github_client.py` to `forge/auth.py` where they
  properly belong alongside the `forge/github.py` that uses them.

### Removed

- `src/radioactive_ralph/github_client.py` (the legacy `GitHubClient` class).
  Dead code — `forge/github.py` was already the real implementation.
- `.claude-plugin/plugin.json` — redundant with `strict: false` marketplace entry.

### Deprecated / stubbed pending rewrite

- `Orchestrator.run()` and `Orchestrator.stop()` raise `NotImplementedError`
  with a pointer to the PRD. Inner helpers (`_merge_ready`, `_review_pending`,
  `_should_discover`) preserved as reusable building blocks for M2.
- `agent_runner.run_parallel_agents()` raises `NotImplementedError`. The
  previous implementation called `claude --message --yes`, which is not a
  real Claude CLI flag. Replacement lands in M2 (stream-json subprocess
  control).
- `ralph run` CLI subcommand exits 2 with the rewrite pointer.

### Fixed

- `tests/test_cli.py::test_main_verbose` had an empty `pass` body; now
  asserts `--verbose` dispatches through `logging.basicConfig` with
  `DEBUG` level.
- `tests/test_orchestrator.py::test_step_spawns_agents` passed
  `repo_name` as a Pydantic kwarg where it's defined as a computed
  property; the test is removed (the underlying `_step` method is
  stubbed pending M2).

## [0.5.1](https://github.com/jbcom/radioactive-ralph/compare/v0.5.0...v0.5.1) (2026-04-10)


### Bug Fixes

* replace misleading comment in doctor.py health check function ([#22](https://github.com/jbcom/radioactive-ralph/issues/22)) ([774166b](https://github.com/jbcom/radioactive-ralph/commit/774166b75a169e04154413769cfe09b5ea321351))

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
