# Contributing to radioactive-ralph

Thanks for your interest in contributing. This is an autonomous development
orchestrator — a Go binary that drives AI agent CLIs (Claude Code, Codex,
OpenCode, Copilot) across repositories 24/7.

## Quick start

```bash
go build ./...
go test ./...
golangci-lint run
```

For Linux-specific tests from macOS:

```bash
make test-linux          # full suite in Docker
make test-linux-agent    # just internal/agent
```

## Architecture overview

Read `AGENTS.md` first — it documents the product contract, the control
invariant (never block), state model, providers, plans, and testing patterns.

The authoritative design spec lives at
`docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md`.

## Branching and merging

- Work on feature branches off `main`.
- Use Conventional Commits: `feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
  `ci:`, `build:`, `chore:`, `perf:`.
- Merges go through the GitHub merge queue with merge commits (history is
  preserved).
- Never commit to `main` directly.

## Testing doctrine

The repo has an extensive testing doctrine in `AGENTS.md`. Key principles:

- A check that finds nothing to check must **FAIL**, not pass.
- Prove a fix by reverting it — confirm the named test fails for the stated
  reason.
- Verify the **artifact**, not the intent. "I made the edit" is not "the file
  changed."
- A new task field ships to **all three renderers** (TUI, GUI, CLI `status`)
  in the same PR.
- Read the output the user gets, not the assertions about it.

## Remote agents

A fleet of agents operates these repositories unattended on Gitea. Work you
do locally can duplicate or conflict with theirs. See
`~/.claude/shared/remote-agents.md` for the full model.

**In short:** hand off dependency bumps and self-contained issues to the
fleet. Do it yourself when the work needs judgment the fleet doesn't have.

## Security reports

Do not file public issues for security vulnerabilities. Use
[GitHub Security Advisories](https://github.com/jbcom/radioactive-ralph/security/advisories/new)
or see `SECURITY.md`.