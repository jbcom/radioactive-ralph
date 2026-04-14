---
title: Testing
updated: 2026-04-14
status: current
domain: quality
---

# Testing — radioactive-ralph

## Strategy

Three layers, each with a distinct purpose:

| Layer | Scope | Gating | Count today |
|-------|-------|--------|-------------|
| Unit | Pure logic, parseable modules | Always on | ~120 |
| Integration (offline) | End-to-end flows with fake subprocesses / fake origin / respx HTTP mocks | Always on | ~20 |
| Integration (online) | Real `claude -p` subprocesses, real `gh` against a fixture repo | Gated on `CLAUDE_AUTHENTICATED=1` env var | 0 today, landing in M4 |

## Run the checks

```bash
hatch test                         # pytest suite
hatch fmt --check                  # ruff format + lint
hatch run hatch-test:type-check    # strict mypy
hatch run docs:build               # docs validation + Sphinx build
```

When `hatch` is unavailable, the `uv` equivalents work:

```bash
uv run --all-extras pytest
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run mypy src/
```

## What CI validates

| Check | Purpose |
|-------|---------|
| `hatch fmt --check` | Formatting + lint hygiene |
| `hatch run hatch-test:type-check` | Strict typing on `src/` |
| `hatch test` | Unit + offline integration tests, coverage artefacts |
| `hatch run docs:build` | Docs structure validation + Sphinx build |

## Conventions

- Use **pytest-mock** via the `mocker` fixture — not `unittest.mock`
- Mark async tests with `@pytest.mark.asyncio` when needed
- Use `tmp_path` for filesystem tests and `tmp_repo` for repo simulation
- Mock at the boundary (subprocess, HTTP client, socket) — not deep internals
- For subprocess tests, prefer asserting on the command line that would be
  spawned rather than on internal methods

## Coverage target

80% line coverage. Exempt: `if TYPE_CHECKING:` blocks and `pragma: no cover`
lines. Coverage drops during M1→M3 are expected (stubbed code paths); the
M4 integration harness restores confidence.

## Integration test gating

Online integration tests require:

- `CLAUDE_AUTHENTICATED=1` in the environment
- `claude` CLI on `PATH`, authenticated
- `gh` CLI on `PATH`, authenticated
- `git` ≥ 2.5

Without all four, the online tests skip cleanly (one-line per-test `pytest.skip`).
Offline integration tests run everywhere using respx (HTTP) and
`subprocess.Popen` mocking.

## Test plan (post-M1 rewrite)

M2 adds `tests/daemon/` covering:
- SQLite event log append + replay
- Unix socket IPC round trips
- Multiplexer detection fallback chain
- `ClaudeSession` stream-json parsing (mocked subprocess)

M3 adds `tests/variants/` covering:
- Every `VariantProfile` loads with valid fields
- Safety-floor enforcement (two-step override paths)
- Tool allowlist subset assertion per variant
- Pre-flight question registry rendering

M4 adds `tests/integration/` covering:
- Full `ralph init` → `ralph run --variant grey` end-to-end against fake origin
- Session death + `claude -p --resume <uuid>` recovery
- Pre-flight refusal flows (old-man on default branch)
- Multiplexer fallback (force-missing tmux/screen)
- LFS detection applying per-variant `lfs_mode`
- Shared-object corruption detection + `git repack -a -d` recovery
- Hook preservation (operator `.git/hooks/pre-commit` copied into mirror)

See the [PRD](../plans/2026-04-14-radioactive-ralph-rewrite.prq.md) for the
full milestone acceptance criteria.
