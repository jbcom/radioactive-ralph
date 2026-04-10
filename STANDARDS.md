---
title: STANDARDS.md — radioactive-ralph
updated: 2026-04-10
status: current
domain: technical
---

# Code Standards — radioactive-ralph

## Non-negotiable constraints

- **300 LOC max per file** — split if needed
- **Python 3.12+** — use modern syntax (`X | Y`, `tomllib`, etc.)
- **mypy strict** — no `Any`, no `# type: ignore` without justification
- **ruff** — formatter and linter, `line-length = 100`
- **pytest-mock** — never `unittest.mock`
- **asyncio.create_subprocess_exec** — never `subprocess.shell=True` or `exec()`
- **Pydantic v2** — use `model_validate`, `model_dump_json`, `Field(default_factory=...)`

## Commit format

Conventional Commits always:
```
feat: add pr list command
fix: handle missing config file gracefully
chore: update deps
docs: add architecture diagram
```

## Git

- SSH remotes only: `git@github.com:jbcom/radioactive-ralph.git`
- Never force push
- Always squash merge PRs

## Security

- Never log API keys or tokens
- Use `asyncio.create_subprocess_exec` (not shell=True) for all subprocess calls
- Config file in `~/.radioactive-ralph/` — never commit credentials
