---
title: Testing
updated: 2026-04-10
status: current
domain: quality
---

# Testing — radioactive-ralph

## Strategy

Unit tests for pure logic. Integration tests require real `gh` CLI + `ANTHROPIC_API_KEY`.
The test suite runs unit tests only in CI — integration tests are opt-in locally.

## Running tests

```bash
hatch test                  # Run unit tests (matrix: py3.12, py3.13)
hatch test --cover          # With coverage report
hatch test -k "test_models" # Run specific tests
hatch run hatch-test:type-check  # mypy strict check
hatch fmt                   # ruff format + check
hatch fmt --check           # Check only (CI mode)
```

## Test inventory

| File | Tests | What it covers |
|------|-------|---------------|
| `test_models.py` | 8 | PRStatus, WorkPriority, PRInfo.is_mergeable, ReviewResult.has_blocking_issues, OrchestratorState defaults |
| `test_state.py` | 5 | load_state (missing file), save/load roundtrip, merge dedup, priority sort, prune |
| `test_work_discovery.py` | 4 | discover_missing_files (all missing, partial, complete), parse_state_md |
| `test_pr_manager.py` | 4 | extract_pr_url, PRInfo.is_mergeable variants, classify_pr (pytest-mock) |

## Conventions

- **pytest-mock** (`mocker` fixture) — never `unittest.mock`
- **`@pytest.mark.asyncio`** for async tests (asyncio_mode = "auto" so often implicit)
- **`tmp_path`** for filesystem tests, **`tmp_repo`** fixture for repo simulation
- Mocking strategy: mock at the boundary (`run_gh`, `asyncio.create_subprocess_exec`), not deep internals

## Coverage target

80% line coverage. Exempt: `if TYPE_CHECKING:` blocks, `pragma: no cover`.

## Integration tests (local only)

Require `ANTHROPIC_API_KEY` and `gh auth status`. Not run in CI.
Mark with `@pytest.mark.integration` and skip unless `--integration` flag passed.
