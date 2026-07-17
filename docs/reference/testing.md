---
title: Testing
lastUpdated: 2026-07-16
---

# Testing — radioactive-ralph

## Strategy

| Layer | Scope | Gating |
|-------|-------|--------|
| Unit | Package-local logic and schema rules | Always on |
| E2E Layer 1 (teatest) | The TUI's `tea.Model` driven through `teatest.NewTestModel` with real `tea.KeyMsg` keystrokes against a `FakeDataSource`, asserting on rendered terminal output | CI-feasible, deterministic |
| E2E Layer 2 (real-binary pty) | A real `--supervisor` process, a real `--init` against a fixture project, a real non-tty client status check, and a real client TUI driven under an actual pty (`creack/pty`) with literal keystroke bytes | CI-feasible, real OS processes |
| Live/manual | Real provider CLI turns against a hosted model, real launchd/systemd/SCM install | Manual / opt-in workflow_dispatch |

Layer 1 (`internal/tui/e2e_test.go`) proves the model logic renders drill
navigation correctly without needing a real supervisor. Layer 2
(`tests/e2e/flow_test.go`, `tests/e2e/pty_driver.go`) proves the whole
path end-to-end — build the real binary, start a real supervisor, `--init`
a fixture project, confirm the client sees it, then drive the TUI under a
real pty exactly as a user's terminal would (arrow keys and Enter as
literal ANSI byte sequences, not `tea.KeyMsg` values). Both layers are
CI-feasible because neither needs a live provider CLI or spends money;
`tests/e2e/live_test.go` is the one true live layer, gated behind
`RALPH_E2E_LIVE=1`, that dispatches a real orchestrator step against a
real installed `claude`/`codex`/`opencode` CLI under a small spend cap —
it skips (not fails) when no supported CLI is on `PATH`.

## Run the checks

```bash
go build ./...
go test ./...
go test -race ./...
golangci-lint run
govulncheck ./...
python3 -m tox -e docs
```

The docs tox environment handles the Sphinx dependencies and the Go API
prebuild in one shot.

## What CI validates

| Check | Purpose |
|-------|---------|
| `go test -race ./...` on Ubuntu and macOS | Unit + E2E Layer 1/2 coverage with the race detector |
| `go test ./...` on Windows | Native Windows coverage, including Windows-specific pty/named-pipe paths |
| `go build ./...` + cross-target test compilation | Compiles the module plus platform-sensitive tests for Linux/macOS/Windows, `amd64`/`arm64` |
| `golangci-lint run` | Lint hygiene |
| `actionlint` | Validates GitHub Actions workflow syntax |
| `govulncheck ./...` | Dependency and call-site vulnerability scan |
| `tox -e docs` | API markdown generation, docs validation, Sphinx build |

## What CI does not prove

CI is intentionally strong on hermetic coverage and weaker on host-manager
integration and live provider behavior:

- live launchd/systemd-user/Windows SCM install/start/stop on a real host
- live provider turns against a real hosted model

`.github/workflows/service-managers.yml` covers the first, opt-in via
`workflow_dispatch` because it needs real host-manager capabilities.
`.github/workflows/provider-live.yml` covers the second, opt-in and
credentialed:

- `ANTHROPIC_API_KEY` for Claude live smoke
- `OPENAI_API_KEY` for Codex live smoke (the workflow runs
  `codex login --with-api-key` headlessly before enabling the test)

`gemini` was removed as a shipped provider on 2026-06-18, so there is no
live Gemini smoke step.

## Conventions

- Keep Go files under the repo's ~300-line discipline where practical.
- Mock at the boundary: subprocesses, IPC, filesystem, external CLIs.
- Prefer deterministic fixtures for provider CLI behavior via cassette
  replay ([Cassette VCR](../guides/cassette-vcr.md)) or fake binaries.
- Use package-level tests for plan-grammar invariants, store schema
  correctness, and orchestrator verification behavior.

## Manual live-provider setup

- the shipped provider CLIs on `PATH`, authenticated:
  `claude`, `codex`, `opencode`
- `gh` CLI on `PATH`, authenticated
- a disposable repo or sandbox directory

Set `RALPH_E2E_LIVE=1` to run `tests/e2e/live_test.go` against whichever
supported CLI is detected on `PATH`. Default CI never depends on a live
provider account; release validation is stricter and should pass without
provider skips for the shipped bindings before a stable tag.

## Current test focus

- `internal/store` schema/migration correctness and spend accounting
- `internal/plan` grammar validation and heuristic decomposition
- `internal/orch` dispatch, spend-cap admission, and verification
- `internal/supervisor` discovery, single-instance, stale-socket reclaim
- `cmd/radioactive_ralph` command wiring
- docs generation and Sphinx publication
