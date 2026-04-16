---
title: Testing
lastUpdated: 2026-04-15
---

# Testing — radioactive-ralph

## Strategy

Three layers, each with a distinct purpose:

| Layer | Scope | Gating | Count today |
|-------|-------|--------|-------------|
| Unit | Package-local logic and schema rules | Always on | Extensive |
| Integration (offline) | End-to-end CLI and subsystem flows with fake processes and fixtures | Always on | Present in `tests/` and `internal/..._test.go` |
| Live/manual | Real provider CLI and `gh` behavior against a local operator environment | Manual | Not part of default CI |

## Run the checks

```bash
go test ./...
golangci-lint run
govulncheck ./...
python3 -m tox -e docs
```

The docs tox environment handles the Sphinx dependencies and the Go API prebuild in one shot:

```bash
python3 -m tox -e docs
```

## What CI validates

| Check | Purpose |
|-------|---------|
| `go test ./...` on Ubuntu, macOS, and Windows | Native unit and integration coverage across the supported host platforms, including repo-service lifecycle and attach-stream smoke over the real local control plane |
| CLI smoke on Ubuntu, macOS, and Windows | Verifies the built binary can execute the core help/doctor/service surface on each native runner |
| Temp-repo smoke on Ubuntu, macOS, and Windows | Verifies repo init, basic plan-surface behavior, and the expected pre-service `status` failure against a fresh repo on each native runner |
| Live repo-service IPC smoke on Ubuntu, macOS, and Windows | Launches `service start` on the native runner, polls `status` over the real local control plane, and verifies graceful `stop` against the live runtime |
| Unix service-definition smoke | Verifies `service install`, `service list`, and `service uninstall` against an isolated `HOME` on macOS and Linux without touching the host service manager |
| `go build ./...` + cross-target test compilation | Compiles the module plus platform-sensitive command/runtime/provider test binaries for Linux, macOS, and Windows on both `amd64` and `arm64` release targets |
| `golangci-lint run` | Lint hygiene |
| `actionlint` | Validates GitHub Actions workflow syntax, matrix structure, and shell-step wiring |
| `govulncheck ./...` | Dependency and call-site vulnerability scan |
| `tox -e docs` | API markdown generation, docs validation, and Sphinx build |

## What CI does not prove

CI is intentionally strong on hermetic coverage and weaker on host-manager
integration. The remaining real-host checks are:

- live launchd install/start/stop behavior on macOS
- live systemd user-unit install/start/stop behavior on Linux
- live Windows SCM install/start/stop behavior on native Windows
- credentialed Gemini runner smoke in a real operator environment

The repo now includes an opt-in GitHub Actions workflow,
`.github/workflows/service-managers.yml`, for native launchd, systemd-user, and
Windows SCM smoke. It is intentionally `workflow_dispatch` only because those
checks depend on host-manager capabilities that are stronger and noisier than
the default CI contract.

The repo also includes an opt-in credentialed provider workflow,
`.github/workflows/provider-live.yml`, that installs the Claude Code, Codex,
and Gemini CLIs and runs the live provider smoke tests when the corresponding
repository secrets are present. That workflow expects:

- `ANTHROPIC_API_KEY` for Claude live smoke
- `OPENAI_API_KEY` for Codex live smoke
- `GEMINI_API_KEY` or `GOOGLE_API_KEY` for Gemini live smoke

The repo still covers the service and IPC contract in hermetic tests by
validating service-manager command construction, the persisted Windows config
payload, the `service run-windows` argv construction, the named-pipe endpoint
derivation, and the CLI's pre-connect heartbeat gate without needing a live
launchd/systemd/SCM instance.

## Conventions

- Keep Go files under the repo's 300-line discipline where practical.
- Mock at the boundary: subprocesses, IPC, filesystem, or external CLIs.
- Prefer deterministic fixtures for provider CLI behavior via cassette replay or fake binaries.
- Use package-level tests to enforce variant invariants, schema correctness, and migration behavior.

## Coverage target

High confidence over vanity percentages. The important gates are variant-profile invariants, plan-store correctness, and safe runtime behavior.

## Integration test gating

Manual live checks require:

- at least one provider CLI on `PATH`, authenticated, depending on which
  binding you want to exercise:
  - `claude`
  - `codex`
  - `gemini`
- `gh` CLI on `PATH`, authenticated
- a disposable repo or sandbox branch

The live smoke tests are opt-in and gated explicitly:

- `CLAUDE_AUTHENTICATED=1` enables the real Claude session and runner tests
- `CODEX_AUTHENTICATED=1` enables the real Codex runner test
- `GEMINI_AUTHENTICATED=1` enables the real Gemini runner test

Claude also accepts `ANTHROPIC_API_KEY` in the environment, and Codex accepts
`OPENAI_API_KEY`, so those tests do not require an interactive CLI login flow
when API-key auth is available.

Gemini also requires `GEMINI_API_KEY` or `GOOGLE_API_KEY` in the environment.

Default CI stays hermetic and does not depend on a live provider account.

## Current test focus

- `internal/db` migration and spend accounting
- `internal/variant` safety floors and model/tool invariants
- `cmd/radioactive_ralph` command wiring
- docs generation and Sphinx publication
