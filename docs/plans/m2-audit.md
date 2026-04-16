---
title: feat/m2-variant-service-cli coverage audit
lastUpdated: 2026-04-16
status: current
orphan: true
---

# m2 Branch Coverage Audit

This document catalogues every commit on `feat/m2-variant-service-cli`
and whether its changes are already present on the
`codex/chore-repo-service-runtime` branch (the live integration
branch).

The audit exists to safely close/delete `feat/m2-variant-service-cli`
without losing work — i.e. to answer the question *"is anything
missing before we drop the branch?"*

## TL;DR

**Nothing missing.** Every m2 commit's payload is either:

1. Present on the integration branch under the same path, OR
2. Present on the integration branch under a renamed path
   (e.g. `internal/session/*` → `internal/provider/claudesession/*`),
   OR
3. Deliberately dropped per the v1 PRD non-goals (skill-wrapper
   SKILL.md files, plugin packaging surface).

## Commit-by-commit map

| m2 commit | Summary | Status on integration branch |
|-----------|---------|------------------------------|
| 9535634 | fixit opus + effort=high + config.toml knobs | **Present.** `internal/config/config.go` has `PlanModel`/`PlanEffort`; `internal/fixit/pipeline.go` consumes them. |
| a6914f1 | fixit refinement loop + session --verbose fix | **Present.** `internal/fixit/refine.go::Refine` + `AcceptedAt` in `history.go`. |
| 8aa11a8 | fixit six-stage pipeline | **Present.** Full Stage 1–6: `intent.go`, `explore.go`, `scorer.go`, `analyze.go`, `validate.go`, `emit.go`, `pipeline.go`, `prompts/advisor.tmpl`, `types.go`. |
| 57ed358 | docs: fixit plan-creation pipeline doc | **Present.** `docs/design/fixit-plan-pipeline.md`. |
| 8091a91 | skills/\*-ralph/SKILL.md files + advisor CLI | **Partial — deliberate.** Advisor CLI is in `cmd/radioactive_ralph/advisor.go`. The `skills/*/SKILL.md` files are **intentionally dropped** — the v1 PRD (§ non-goals) explicitly removes skill-wrapper packaging as a product surface. |
| d372468 | split oversized Go files (≤300 LOC rule) | **Present.** Ongoing discipline; all current files honor the limit. |
| 04d4384 | joe-fixit → fixit rename | **Present.** No residual `joe-fixit` references in source or docs on the integration branch. |
| 2827f1c | session cassette-replay VCR | **Present — relocated.** Moved to `internal/provider/claudesession/cassette/` when the provider abstraction landed. |
| f6d7d38 | PR #31 review feedback | **Present.** All addressed inline in the current `internal/variant/` + hook tree. |
| 71e9b1c | integration test harness + gated live claude tests | **Present.** `tests/integration/integration_test.go` + `live_test.go`. |
| d17efaa | kong subcommand tree | **Present — expanded.** `cmd/radioactive_ralph/main.go` has a superset: init, run, status, attach, stop, doctor, service, plan, tui, advisor. |
| 2fb2477 | initcmd capability wizard | **Present.** `internal/initcmd/`. |
| 498dbf3 | supervisor PID flock + event replay + IPC dispatch | **Present — evolved.** Moved to `internal/runtime/` for the repo-scoped service model; flock lives in `runtime/flock*.go`. |
| 58f9b4c | launchd + systemd-user installer with safety gates | **Present.** `internal/service/`. |
| 9f16ea3 | session CI — unauthenticated claude smoke | **Present.** `tests/integration/` + `provider_live_test.go`. |
| 6e53f03 | ClaudeSession stream-json wrapper + PromptRenderer | **Present — relocated.** `internal/provider/claudesession/session.go` + `internal/runtime/prompt.go`. |
| dec260c | workspace mirror + worktree + LFS + hook-copy | **Present.** `internal/workspace/`. |
| ed3c516 | docs: Profile rename + plans-first discipline | **Present.** Across design + guides docs. |
| 141022c | register all ten variant profiles; joe-fixit → fixit | **Present.** `internal/variant/*.go` registers all ten; no `joe-fixit` remnants. |

## Paths that moved

When the provider abstraction landed (September 2026 in runtime-design
terms, commit-level visible on this integration branch), the
session-specific code was re-homed under `internal/provider/` to make
the provider contract portable across `claude`, `codex`, and `gemini`.
Concretely:

| m2 path | Integration-branch path |
|---------|-------------------------|
| `internal/session/session.go` | `internal/provider/claudesession/session.go` |
| `internal/session/lifecycle.go` | `internal/provider/claudesession/lifecycle.go` |
| `internal/session/cassette/` | `internal/provider/claudesession/cassette/` |
| `internal/session/cassette/replayer/` | `internal/provider/claudesession/cassette/replayer/` |
| `internal/session/internal/fakeclaude/` | `internal/provider/claudesession/internal/fakeclaude/` |
| `cmd/ralph/*.go` | `cmd/radioactive_ralph/*.go` |

These are renames, not drops.

## Paths that were intentionally dropped

| m2 path | Reason |
|---------|--------|
| `skills/blue-ralph/SKILL.md` | v1 PRD § non-goals: "reintroducing Python-era daemon, multiplexer, or skill-wrapper behavior" |
| `skills/fixit-ralph/SKILL.md` | same |
| `skills/green-ralph/SKILL.md` | same |
| `skills/grey-ralph/SKILL.md` | same |
| `skills/immortal-ralph/SKILL.md` | same |
| `skills/old-man-ralph/SKILL.md` | same |
| `skills/professor-ralph/SKILL.md` | same |
| `skills/red-ralph/SKILL.md` | same |
| `skills/savage-ralph/SKILL.md` | same |
| `skills/world-breaker-ralph/SKILL.md` | same |

The product surface is now one binary + one repo-scoped runtime; the
skill-wrapper packaging layer is gone.

## Python prototype (`reference/`) coverage

Separate audit because the user asked. Python prototype modules and
their Go coverage status:

| Python module | Go coverage | Status |
|---------------|-------------|--------|
| `agent_runner.py` | `internal/runtime/` (claim-dispatch loop) | **Ported — evolved.** Fresh architecture per PRD. |
| `orchestrator.py` | `internal/runtime/service.go` | **Ported — evolved.** Main orchestration. |
| `cli.py` | `cmd/radioactive_ralph/` | **Ported — expanded** (4 cmds → 10 cmds). |
| `config.py` | `internal/config/config.go` | **Ported.** |
| `doctor.py` | `internal/doctor/doctor.go` | **Ported.** |
| `logging_setup.py` | `internal/rlog/rlog.go` | **Ported — slog over Rich.** |
| `ralph_says.py` | `internal/voice/voice.go` | **Ported.** |
| `models.py` | `internal/plandag/` + `internal/variant/` | **Ported — evolved.** SQLite DAG replaces JSON state. |
| `state.py` | `internal/plandag/` + `internal/runtime/` | **Ported — evolved.** Durable SQLite + repo-service bookkeeping. |
| `git_client.py` | (inside worktree, via `gh` + shell) | **Intentionally dropped** — rewrite PRD § 104: daemon stays out of forge APIs; Claude uses `gh` CLI in worktrees. |
| `pr_manager.py` | (inside worktree) | **Intentionally dropped.** Same reason. |
| `reviewer.py` | (inside worktree) | **Intentionally dropped.** Same reason. |
| `work_discovery.py` | (inside worktree) | **Intentionally dropped.** Same reason. |
| `forge/base.py` + `github.py` + `gitea.py` + `gitlab.py` + `auth.py` | — | **Intentionally dropped.** The Go daemon doesn't speak forge APIs. |
| `dashboard.py` (Rich console) | `cmd/radioactive_ralph/tui.go` (bubbletea) | **Ported — evolved.** |

Rewrite PRD reference (`docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`):

- Line 76–77: "Everything else — code review, PR classification, work
  discovery — is deferred / moved inside worktree."
- Line 104–105: "Preserved Python modules that violated this
  (`reviewer.py`, `pr_manager.py`, `work_discovery.py`, `forge/*`)
  are **not ported to the Go daemon**."

So `reference/` is not a port TODO list — it's a deliberately-archived
prior implementation whose scope narrowed during the rewrite.

## Conclusion

- `feat/m2-variant-service-cli` can be closed/deleted safely. Every
  change is either present on the integration branch, present under a
  renamed path, or deliberately dropped per the v1 PRD.
- `reference/` is not missing any ports against the v1 scope. It can
  stay as labeled archive per PRD § 4.7, or be deleted entirely once
  the v1 PRD launch-gate tasks are done.
