---
title: CHANGELOG
lastUpdated: 2026-04-15
---

# Changelog

All notable changes to this project will be documented in this file.
Format: [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), [Semantic Versioning](https://semver.org/).

Older entries preserve the product language that was true when those releases
shipped. That means historical sections may still mention MCP, plugins,
supervisors, or the archived Python implementation even though those are no
longer part of the live contract.

## [0.21.6](https://github.com/jbcom/radioactive-ralph/compare/v0.21.5...v0.21.6) (2026-07-17)


### Bug Fixes

* **provider:** bound declarative turns + reap their process tree ([#139](https://github.com/jbcom/radioactive-ralph/issues/139)) ([925d4db](https://github.com/jbcom/radioactive-ralph/commit/925d4dbeec739868fdb4848b1bd50a25e6bf26c4))

## [0.21.5](https://github.com/jbcom/radioactive-ralph/compare/v0.21.4...v0.21.5) (2026-07-17)


### Bug Fixes

* **agent:** kill the whole process group, not just the direct child ([#138](https://github.com/jbcom/radioactive-ralph/issues/138)) ([2847d64](https://github.com/jbcom/radioactive-ralph/commit/2847d641f5acbfd82f57b1863834e8c24604f94f))

## [0.21.4](https://github.com/jbcom/radioactive-ralph/compare/v0.21.3...v0.21.4) (2026-07-17)


### Bug Fixes

* **agent:** stop the watchdog goroutine after a stall (goroutine leak) ([#136](https://github.com/jbcom/radioactive-ralph/issues/136)) ([e24aa51](https://github.com/jbcom/radioactive-ralph/commit/e24aa51a242aff478fa9ea8a53c62625a31e27b7))

## [0.21.3](https://github.com/jbcom/radioactive-ralph/compare/v0.21.2...v0.21.3) (2026-07-17)


### Bug Fixes

* **orch:** reserve capped-provider spend before concurrent launches ([#131](https://github.com/jbcom/radioactive-ralph/issues/131)) ([637d8dd](https://github.com/jbcom/radioactive-ralph/commit/637d8dd3ab88e5411b84fd778761b213ff047379))

## [0.21.2](https://github.com/jbcom/radioactive-ralph/compare/v0.21.1...v0.21.2) (2026-07-17)


### Bug Fixes

* **store:** cap the SQLite pool at one connection ([#129](https://github.com/jbcom/radioactive-ralph/issues/129)) ([531f9c7](https://github.com/jbcom/radioactive-ralph/commit/531f9c7963651badf3b0ebdcb500dab90262f1dd))

## [0.21.1](https://github.com/jbcom/radioactive-ralph/compare/v0.21.0...v0.21.1) (2026-07-17)


### Bug Fixes

* **orch:** dispatch provider turns asynchronously (never-block invariant) ([#127](https://github.com/jbcom/radioactive-ralph/issues/127)) ([f4627eb](https://github.com/jbcom/radioactive-ralph/commit/f4627ebb9074720191c1a08b623c865d778631bb))

## [0.21.0](https://github.com/jbcom/radioactive-ralph/compare/v0.20.0...v0.21.0) (2026-07-17)


### Features

* **gui:** confirm before abandoning a plan or killing a worker ([#122](https://github.com/jbcom/radioactive-ralph/issues/122)) ([1b23218](https://github.com/jbcom/radioactive-ralph/commit/1b23218ff811e5f097bc9d45330d2413e0b9fbb8))


### Bug Fixes

* **doctor:** distinguish missing claude CLI from unauthenticated ([#125](https://github.com/jbcom/radioactive-ralph/issues/125)) ([5e7ef40](https://github.com/jbcom/radioactive-ralph/commit/5e7ef403110f0db924c0399839439167b4bff694))
* **gui:** scroll to top when the drill view changes ([#123](https://github.com/jbcom/radioactive-ralph/issues/123)) ([6ce981d](https://github.com/jbcom/radioactive-ralph/commit/6ce981de211149d9ae97f819ab06f554d7ebc2ea))

## [0.20.0](https://github.com/jbcom/radioactive-ralph/compare/v0.19.1...v0.20.0) (2026-07-17)


### Features

* **doctor:** verify the XDG state root is usable ([#120](https://github.com/jbcom/radioactive-ralph/issues/120)) ([997f701](https://github.com/jbcom/radioactive-ralph/commit/997f7018d75a09f58d6716feabfd3c28699496a7))

## [0.19.1](https://github.com/jbcom/radioactive-ralph/compare/v0.19.0...v0.19.1) (2026-07-17)


### Bug Fixes

* **gui:** coordinate drive-action errors with the paint loop ([#119](https://github.com/jbcom/radioactive-ralph/issues/119)) ([9788d20](https://github.com/jbcom/radioactive-ralph/commit/9788d20669a86e33d37b504a81d41483c1ae9c72))

## [0.19.0](https://github.com/jbcom/radioactive-ralph/compare/v0.18.0...v0.19.0) (2026-07-17)


### Features

* **gui:** focus the first action after each drill render (a11y) ([#116](https://github.com/jbcom/radioactive-ralph/issues/116)) ([e7c473f](https://github.com/jbcom/radioactive-ralph/commit/e7c473f6cf60843ec82e6552412b09a6ae78e468))

## [0.18.0](https://github.com/jbcom/radioactive-ralph/compare/v0.17.1...v0.18.0) (2026-07-17)


### Features

* **doctor:** surface the codex spend-cap metering blind spot ([#112](https://github.com/jbcom/radioactive-ralph/issues/112)) ([cacdc63](https://github.com/jbcom/radioactive-ralph/commit/cacdc631d83a1d068d94fc1284017b421a202479))

## [0.17.1](https://github.com/jbcom/radioactive-ralph/compare/v0.17.0...v0.17.1) (2026-07-17)


### Bug Fixes

* **ci:** pin a well-formed locale for the GUI test (Fyne harfbuzz panic) ([#110](https://github.com/jbcom/radioactive-ralph/issues/110)) ([49f78d1](https://github.com/jbcom/radioactive-ralph/commit/49f78d15c56b13c650e853fe943e5d924d4f56cc))

## [0.17.0](https://github.com/jbcom/radioactive-ralph/compare/v0.16.2...v0.17.0) (2026-07-17)


### Features

* **gui:** Escape-to-drill-back keyboard navigation ([#107](https://github.com/jbcom/radioactive-ralph/issues/107)) ([98dcce6](https://github.com/jbcom/radioactive-ralph/commit/98dcce6ab1cbec5856a6b1e8b2d75d9dac6d9978))

## [0.16.2](https://github.com/jbcom/radioactive-ralph/compare/v0.16.1...v0.16.2) (2026-07-17)


### Bug Fixes

* **packaging:** macOS cask ships both arm64 + amd64 (Intel Macs) ([#106](https://github.com/jbcom/radioactive-ralph/issues/106)) ([68ca1e6](https://github.com/jbcom/radioactive-ralph/commit/68ca1e6a6512e9cdd420336f1db4c1d99cbd4aaf))

## [0.16.1](https://github.com/jbcom/radioactive-ralph/compare/v0.16.0...v0.16.1) (2026-07-17)


### Bug Fixes

* **gui,packaging:** AppImage FUSE, project-agnostic import, stale-paint, count labels ([#102](https://github.com/jbcom/radioactive-ralph/issues/102)) ([860ad08](https://github.com/jbcom/radioactive-ralph/commit/860ad08b521c2655f639d70dd734234e81e88147))

## [0.16.0](https://github.com/jbcom/radioactive-ralph/compare/v0.15.0...v0.16.0) (2026-07-17)


### Features

* **tui:** supervisor-liveness line in the macro header ([#98](https://github.com/jbcom/radioactive-ralph/issues/98)) ([a0addbe](https://github.com/jbcom/radioactive-ralph/commit/a0addbe23337e6ff059564ff60d58b360d4c2d1f))

## [0.15.0](https://github.com/jbcom/radioactive-ralph/compare/v0.14.0...v0.15.0) (2026-07-17)


### Features

* **gui:** recent-activity project-events feed (TUI parity) ([#96](https://github.com/jbcom/radioactive-ralph/issues/96)) ([ad99986](https://github.com/jbcom/radioactive-ralph/commit/ad9998699886656e2164cfa99771af2cf428c63d))

## [0.14.0](https://github.com/jbcom/radioactive-ralph/compare/v0.13.0...v0.14.0) (2026-07-17)


### Features

* native installers & GUI desktop packaging ([#92](https://github.com/jbcom/radioactive-ralph/issues/92)) ([a1df782](https://github.com/jbcom/radioactive-ralph/commit/a1df782295d6bf9fdadba8fd157633bc0b057eb3))

## [0.13.0](https://github.com/jbcom/radioactive-ralph/compare/v0.12.0...v0.13.0) (2026-07-17)


### Features

* Fyne desktop GUI client ([#89](https://github.com/jbcom/radioactive-ralph/issues/89)) ([e969551](https://github.com/jbcom/radioactive-ralph/commit/e969551bbb93a5cc929f36e8eb84ceb23c67c33b))

## [0.12.0](https://github.com/jbcom/radioactive-ralph/compare/v0.11.0...v0.12.0) (2026-07-17)


### Features

* versioned IPC drive+observe API ([#87](https://github.com/jbcom/radioactive-ralph/issues/87)) ([2f20adf](https://github.com/jbcom/radioactive-ralph/commit/2f20adfa36373df9cb03aa54e6129dba75553761))

## [0.11.0](https://github.com/jbcom/radioactive-ralph/compare/v0.10.4...v0.11.0) (2026-07-17)


### Features

* guided first-run onboarding wizard ([#85](https://github.com/jbcom/radioactive-ralph/issues/85)) ([80daad9](https://github.com/jbcom/radioactive-ralph/commit/80daad9cf480df90fda322240e4269cba0befc29))

## [0.10.4](https://github.com/jbcom/radioactive-ralph/compare/v0.10.3...v0.10.4) (2026-07-17)


### Bug Fixes

* cassette pump-join — final audit convergence fix ([#83](https://github.com/jbcom/radioactive-ralph/issues/83)) ([eedb6d3](https://github.com/jbcom/radioactive-ralph/commit/eedb6d3555f5b13c9b582f47f660b12d44684473))

## [0.10.3](https://github.com/jbcom/radioactive-ralph/compare/v0.10.2...v0.10.3) (2026-07-17)


### Bug Fixes

* resolve all 5 findings from the third convergence audit ([#81](https://github.com/jbcom/radioactive-ralph/issues/81)) ([a75abd3](https://github.com/jbcom/radioactive-ralph/commit/a75abd3d60c1e9fb1eedd7ca859138f8438b7dcd))

## [0.10.2](https://github.com/jbcom/radioactive-ralph/compare/v0.10.1...v0.10.2) (2026-07-17)


### Bug Fixes

* resolve all 14 findings from the second-pass audit of the fix code ([#79](https://github.com/jbcom/radioactive-ralph/issues/79)) ([f8b3de2](https://github.com/jbcom/radioactive-ralph/commit/f8b3de2f4cec971010b8ea398f12b7ccb3c617c3))

## [0.10.1](https://github.com/jbcom/radioactive-ralph/compare/v0.10.0...v0.10.1) (2026-07-17)


### Bug Fixes

* resolve all 29 findings from the post-release multi-lens audit ([#76](https://github.com/jbcom/radioactive-ralph/issues/76)) ([e8268db](https://github.com/jbcom/radioactive-ralph/commit/e8268db5aa37bb95dbdfe76fef03471a4fd17486))

## [0.10.0](https://github.com/jbcom/radioactive-ralph/compare/v0.9.1...v0.10.0) (2026-07-17)


### Features

* rebuild as a supervised-execution runtime (supervisor architecture) ([#73](https://github.com/jbcom/radioactive-ralph/issues/73)) ([00c788d](https://github.com/jbcom/radioactive-ralph/commit/00c788d397494a45f16fdd21aeb888314a46d407))

## [0.9.1](https://github.com/jbcom/radioactive-ralph/compare/v0.9.0...v0.9.1) (2026-07-16)


### Bug Fixes

* **cassette:** data race on recorder start time ([#70](https://github.com/jbcom/radioactive-ralph/issues/70)) ([ab8fa9f](https://github.com/jbcom/radioactive-ralph/commit/ab8fa9fab24015aea781398fcabd2db7bba6f04b))

## [0.9.0](https://github.com/jbcom/radioactive-ralph/compare/v0.8.3...v0.9.0) (2026-07-16)


### Features

* **provider:** remove gemini as a shipped provider (deprecated 2026-06-18) ([#66](https://github.com/jbcom/radioactive-ralph/issues/66)) ([26a433e](https://github.com/jbcom/radioactive-ralph/commit/26a433e423a5d90c946f92b18fbdc326ab9bfa32))

## [0.8.3](https://github.com/jbcom/radioactive-ralph/compare/v0.8.2...v0.8.3) (2026-07-16)


### Bug Fixes

* close 4 critical durable-runtime safety gaps ([#63](https://github.com/jbcom/radioactive-ralph/issues/63)) ([c0f48b2](https://github.com/jbcom/radioactive-ralph/commit/c0f48b26ab7551668bf2af29e6649e9728a547be))

## [0.8.2](https://github.com/jbcom/radioactive-ralph/compare/v0.8.1...v0.8.2) (2026-04-16)


### Bug Fixes

* **docs:** use explicit-URL `brew tap` form ([#44](https://github.com/jbcom/radioactive-ralph/issues/44)) ([2e39c5e](https://github.com/jbcom/radioactive-ralph/commit/2e39c5ed3896b7ecab31d754031b424ef2f56a70))

## [0.8.1](https://github.com/jbcom/radioactive-ralph/compare/v0.8.0...v0.8.1) (2026-04-16)


### Bug Fixes

* **release:** goreleaser opens PRs on jbcom/pkgs instead of direct push ([#42](https://github.com/jbcom/radioactive-ralph/issues/42)) ([d7233b6](https://github.com/jbcom/radioactive-ralph/commit/d7233b6f70f31328b5b59554900027fa38b42292))

## [0.8.0](https://github.com/jbcom/radioactive-ralph/compare/v0.7.0...v0.8.0) (2026-04-16)


### Features

* ship repo service runtime ([#40](https://github.com/jbcom/radioactive-ralph/issues/40)) ([ba538cd](https://github.com/jbcom/radioactive-ralph/commit/ba538cd61e8cca21db66bbfd9cedef46c261d94d))

## [0.7.0](https://github.com/jbcom/radioactive-ralph/compare/v0.6.1...v0.7.0) (2026-04-15)


### Features

* omnibus — repo-service runtime polish, release fixes, docs ([#36](https://github.com/jbcom/radioactive-ralph/issues/36)) ([30db744](https://github.com/jbcom/radioactive-ralph/commit/30db744515c325e836c983186284a048234eeb03))

## [0.6.1](https://github.com/jbcom/radioactive-ralph/compare/v0.6.0...v0.6.1) (2026-04-15)


### Bug Fixes

* **ci:** use GitHub native auto-merge for bots ([#35](https://github.com/jbcom/radioactive-ralph/issues/35)) ([2722c20](https://github.com/jbcom/radioactive-ralph/commit/2722c20fa703ff38c6630f1f0da1af31fdbd758f))
* **release:** collapse to jbcom/pkgs + single CI_GITHUB_TOKEN ([#33](https://github.com/jbcom/radioactive-ralph/issues/33)) ([a086067](https://github.com/jbcom/radioactive-ralph/commit/a086067ced6145ad0aecbe6f39fdd8d0b590f8fd))

## [0.6.0](https://github.com/jbcom/radioactive-ralph/compare/v0.5.1...v0.6.0) (2026-04-15)


### Features

* M2 + M3 — Go rewrite, plandag, MCP, Starlight docs, packaging ([#32](https://github.com/jbcom/radioactive-ralph/issues/32)) ([4ed2819](https://github.com/jbcom/radioactive-ralph/commit/4ed28196b671e3fa092e0a3126185b6e02baedd5))
* **m2:** doctor + voice layers ([#30](https://github.com/jbcom/radioactive-ralph/issues/30)) ([5989700](https://github.com/jbcom/radioactive-ralph/commit/5989700012eea04cb3fc1fea7ed88aa90aa25d9c))
* **m2:** foundation — Go rewrite Python→reference + xdg/config/inventory/db layers ([#27](https://github.com/jbcom/radioactive-ralph/issues/27)) ([325f095](https://github.com/jbcom/radioactive-ralph/commit/325f095be532aa96c7d0bab72ac4be17321b9d5b))
* **m2:** multiplexer + ipc layers ([#29](https://github.com/jbcom/radioactive-ralph/issues/29)) ([51d90a7](https://github.com/jbcom/radioactive-ralph/commit/51d90a7beb65b32dd5add7d9fc1adefab5443bc6))

## Historical Appendix — M2 Rewrite Planning Snapshot

### Added — M2 rewrite (PR #31)

- **`internal/variant`** — all ten `Profile` definitions (blue, grey, green,
  red, professor, fixit, immortal, savage, old-man, world-breaker) with
  safety-floor enforcement. New `ShellExplicitlyTrusted` field catches the
  shared+Bash defense-in-depth hole Amazon Q flagged: Bash can run
  `git commit` and arbitrary subprocesses, so shared-isolation variants
  must explicitly opt into it.
- **`internal/workspace`** — mirror clone with `--reference`, worktree pool
  with monotonic branch naming, LFS per-mode config, hook copy preserving
  executable bit. Four orthogonal knobs (isolation/object_store/sync/LFS).
- **`internal/provider/claudesession`** — ClaudeSession wrapping `claude -p
  --input-format stream-json`. Session-ID pinning, sentinel re-prompt on
  resume, PromptRenderer combining variant biases with inventory-selected
  skills. Three test tiers: fake-claude unit tests, cassette-replay
  (deterministic VCR, no auth), gated live tests.
- **`internal/provider/claudesession/cassette`** — subprocess-level VCR. Recorder wraps
  a real claude subprocess and tees stdin+stdout to a JSON cassette;
  replayer binary replays the cassette in CI without credentials.
- **`internal/service`** — launchd + systemd-user unit installers with
  safety gates. Refuses `RefuseServiceContext` variants; requires explicit
  `GateConfirmed` for gated variants.
- **`internal/supervisor`** — PID flock, event replay, IPC dispatch,
  graceful shutdown. Integrates db + ipc + workspace.
- **`internal/initcmd`** — `radioactive_ralph init` capability wizard. Scaffolds
  `.radioactive-ralph/{config,local,plans/index.md}`, idempotent
  `.gitignore` updates, `--refresh` preserves operator choices.
- **`cmd/ralph`** — full Kong CLI (init/run/status/attach/stop/doctor/
  service + hidden `_supervisor`) with signal handling, plans-first
  discipline enforcement, and claude-binary pre-check.
- **`tests/integration`** — always-on end-to-end CLI harness plus gated
  live tests (`CLAUDE_AUTHENTICATED=1`).

### Changed

- `joe-fixit-ralph` renamed to `fixit-ralph` across code, skills, docs,
  and marketplace. Fixit is now the sole variant permitted to recommend
  peers via advisor mode; every other variant refuses to run without a
  valid `.radioactive-ralph/plans/index.md`.
- Task-batch PreCompact hook redirected from `$REPO/.claude/state/` to
  `$XDG_STATE_HOME/claude-code/task-batch` (Linux) or
  `~/Library/Application Support/claude-code/task-batch` (macOS) to
  respect the project rule that state lives outside the repo.
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
- `radioactive_ralph run` CLI subcommand exits 2 with the rewrite pointer.

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

## Historical Appendix — Initial Python-Era Unreleased Snapshot

### Added
- Initial Python package structure with hatchling build
- `Orchestrator` async daemon loop with 8-phase cycle
- `PRManager` — gh CLI wrapper for PR classification and merge
- `Reviewer` — internal code review via Anthropic API (haiku/sonnet tiering)
- `WorkDiscovery` — scans repos for missing docs, reads STATE.md and DESIGN.md
- `AgentRunner` — spawns claude CLI subprocesses with model selection
- `State` — durable JSON persistence with dedup and pruning
- `AutoloopConfig` — TOML-based config with sensible defaults
- Click CLI: `radioactive_ralph run`, `radioactive_ralph status`, `ralph discover`, `ralph pr list/merge`, `radioactive_ralph stop`
- `uvx radioactive-ralph` support for zero-install execution
- Sphinx documentation with RTD theme, published to GitHub Pages
- CI/CD: GitHub Actions with OIDC PyPI publishing and Sphinx Pages deploy
- release-please for automated changelog and versioning
- dependabot for weekly dependency updates
