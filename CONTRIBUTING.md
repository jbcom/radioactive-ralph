---
title: Contributing
updated: 2026-04-10
status: current
---

# Contributing to radioactive-ralph

Thanks for your interest in contributing! radioactive-ralph is an autonomous orchestrator for Claude Code — contributions of all kinds are welcome.

## Before you start

- Read [AGENTS.md](AGENTS.md) for the full operating protocol and architecture.
- Read [STANDARDS.md](STANDARDS.md) for code quality rules.
- Open an issue before starting large features — alignment first saves rework.

## Development setup

```bash
# Clone and install in editable mode with all dev deps
git clone git@github.com:jbcom/radioactive-ralph.git
cd radioactive-ralph
uv sync

# Run tests
uv run pytest tests/ -v

# Lint and format
uv run ruff check src/ tests/
uv run ruff format src/ tests/

# Type check
uv run mypy src/
```

## Workflow

1. Fork the repo and create a branch: `git checkout -b feat/my-feature`
2. Write tests first, then implementation (see [STANDARDS.md](STANDARDS.md))
3. Ensure all checks pass: `pytest`, `ruff`, `mypy`
4. Open a pull request against `main` — fill out the PR template

## Pull request standards

- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`
- One logical change per PR — keep diffs reviewable
- All CI checks must pass before merge
- Squash-merge is used — branch history is not preserved

## Code standards

- **Max 300 LOC per file** — decompose by responsibility
- **Google-style docstrings** on all modules, classes, and public methods
- **No stubs, no TODOs, no `pass` bodies** — implement fully or don't open the PR
- **Tests required** for all non-trivial logic; integration tests for real I/O
- **Type annotations** on all public APIs (`mypy --strict` must pass)

## Forge compatibility

radioactive-ralph supports GitHub, GitLab, Gitea, and Forgejo. When touching forge-related code:

- Implement changes in all three forge clients (`forge/github.py`, `forge/gitlab.py`, `forge/gitea.py`)
- Add tests that mock `ForgeClient` — do not test against live APIs
- Verify the `detect_forge()` URL parser handles both SSH and HTTPS remotes

## Configuration

All tunables live in `AutoloopConfig` (pydantic-settings). New config fields must:

- Have a sensible default
- Have a `description=` string
- Be overridable via `RALPH_<FIELD_NAME>` env var (automatic via `env_prefix`)

## Reporting issues

- **Bugs**: Open a GitHub issue with steps to reproduce, Python version, and log output
- **Security vulnerabilities**: See [SECURITY.md](SECURITY.md) — do NOT open a public issue

## Questions

Open a [GitHub Discussion](https://github.com/jbcom/radioactive-ralph/discussions) for general questions.
