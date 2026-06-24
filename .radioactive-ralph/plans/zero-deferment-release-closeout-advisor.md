---
title: Fixit advisor report - zero-deferment-release-closeout
updated: 2026-06-24
status: current
domain: product
variant_recommendation: green
variant_alternate: fixit
confidence: 92
---

# Fixit advisor - zero-deferment-release-closeout

## Operator intent

Bring the active zero-deferment release-readiness milestone to a locally proven
close. Review docs, directives, plans, tests, and code; address all actionable
remote PR feedback; resolve the PR review threads once the branch contains the
fixes; and leave a current plan covering the remaining closeout work.

## Current PR feedback

PR #47 has two unresolved, non-outdated review threads from Gemini Code Assist,
both in `internal/provider/declarative.go`:

- `renderArgTemplate` must render deterministically and must not recursively
  replace token-looking text from user/provider values.
- The `stream-json` scanner line limit should be large enough for unusually
  large LLM frames and tool outputs.

CodeRabbit skipped review while the PR was draft. Amazon Q did not complete a
review because the PR head changed during its run.

## Execution plan

Use `green` for implementation and verification. Use `fixit` only if the
scope changes enough to need a fresh planning bridge.

## Acceptance criteria

- Remote actionable review feedback is addressed in code and tests.
- Repo-visible plan artifacts do not leave obsolete MCP/plugin/slash-command
  framing as current execution guidance.
- `go test ./...`, `go vet ./...`, `golangci-lint run`,
  `bash scripts/validate-docs.sh`, and `python3 -m tox -e docs` pass locally.
- Release-tool checks that are installed locally are run or explicitly called
  out if blocked.
- The PR branch is pushed, review threads are resolved, and the final PR state
  is reported with any external gates still outstanding.
