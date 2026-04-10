---
title: Testing
updated: 2026-04-10
status: current
domain: quality
---

# Testing — radioactive-ralph

## Strategy

Unit tests cover pure logic and repo plumbing. Integration tests are still local-only because they require real `gh` and Claude credentials.

## Run the checks

```bash
hatch fmt --check      # Ruff format + lint
hatch run hatch-test:type-check   # mypy strict mode
hatch test             # pytest suite
hatch run docs:build   # docs validation + Sphinx build
```

## What CI validates

| Check | Purpose |
|---|---|
| `hatch fmt --check` | Formatting and repo-wide lint hygiene |
| `hatch run hatch-test:type-check` | Strict typing on `src/` |
| `hatch test` | Unit tests and coverage artifacts |
| `hatch run docs:build` | Docs structure validation plus Sphinx build |

## Conventions

- Use **pytest-mock** via the `mocker` fixture — not `unittest.mock`
- Mark async tests with `@pytest.mark.asyncio` when needed
- Use `tmp_path` for filesystem tests and `tmp_repo` for repo simulation
- Mock at the boundary (`run_gh`, `asyncio.create_subprocess_exec`), not deep internals

## Coverage target

80% line coverage. Exempt: `if TYPE_CHECKING:` blocks and `pragma: no cover` lines.

## Integration tests

Integration coverage remains opt-in locally. It requires `ANTHROPIC_API_KEY`, `gh auth status`, and a real Claude environment.
