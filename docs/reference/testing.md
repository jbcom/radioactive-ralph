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
| Live/manual | Real `claude` and `gh` behavior against a local operator environment | Manual | Not part of default CI |

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
| `go test ./...` | Unit and integration coverage across the Go codebase |
| `golangci-lint run` | Lint hygiene |
| `govulncheck ./...` | Dependency and call-site vulnerability scan |
| `tox -e docs` | API markdown generation, docs validation, and Sphinx build |

## Conventions

- Keep Go files under the repo's 300-line discipline where practical.
- Mock at the boundary: subprocesses, IPC, filesystem, or external CLIs.
- Prefer deterministic fixtures for `claude -p` behavior via cassette replay.
- Use package-level tests to enforce variant invariants, schema correctness, and migration behavior.

## Coverage target

High confidence over vanity percentages. The important gates are variant-profile invariants, plan-store correctness, and safe supervisor behavior.

## Integration test gating

Manual live checks require:

- `claude` CLI on `PATH`, authenticated
- `gh` CLI on `PATH`, authenticated
- a disposable repo or sandbox branch

Default CI stays hermetic and does not depend on a live Claude account.

## Test plan (post-M1 rewrite)

- `internal/db` migration and spend accounting
- `internal/variant` safety floors and model/tool invariants
- `cmd/radioactive_ralph` command wiring
- docs generation and Sphinx publication
