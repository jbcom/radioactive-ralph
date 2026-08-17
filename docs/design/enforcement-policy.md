---
title: Canonical enforcement policy
description: Versioned adapter-neutral lifecycle, acceptance, wait, and retry contract.
---

# Canonical enforcement policy

Claude, Codex, and OpenCode need the same answer to a supervision event. That
answer must not depend on prompt wording, hook exit conventions, a provider's
idea of completion, or which adapter happens to receive the event.

The `internal/enforcement` package is the canonical executable contract. Its
versioned JSON interchange schema is
[`policy-v1.schema.json`](../../internal/enforcement/schema/policy-v1.schema.json).
Adapters validate and normalize observations, then submit the same `Request` to
the deterministic evaluator. They must not copy or reimplement the transition
table.

This is a foundation, not a global installation. No Claude, Codex, OpenCode, or
user configuration is changed by this package.

## Finite lifecycle

The only persisted states are:

| State | Meaning | Exit condition |
|---|---|---|
| `ACTIVE` | Work may proceed. | Progress, an explicit wait, a verification request, or a bounded failure. |
| `WAIT_EXTERNAL` | A named external owner currently controls progress. | The explicit recheck time arrives before the deadline. |
| `VERIFY` | Work has stopped changing and acceptance evidence is being checked. | Every declared predicate passes, or verification consumes a retry. |
| `COMPLETE` | The evaluator verified every acceptance predicate. | None; terminal and absorbing. |
| `BLOCKED` | A deadline, budget, terminal failure, invalid transition, or malformed input stopped safe progress. | None; terminal and absorbing. |

`COMPLETE` is reachable only from `VERIFY` through a `VERIFY` event whose
results contain every configured predicate with both `observed: true` and
`satisfied: true`. A provider message, process exit, hook success, or agent
assertion is not an acceptance predicate by itself.

## Events and transitions

The event vocabulary is also finite:

- `PROGRESS` resets the consecutive no-progress count.
- `NO_PROGRESS` increments that count and blocks after the configured budget.
- `RETRYABLE_FAILURE` increments the retry count and blocks after its budget.
- `TERMINAL_FAILURE` blocks immediately without accepting provider diagnostics.
- `WAIT_EXTERNAL` requires a normalized owner, a future recheck time, and a
  later hard deadline.
- `RECHECK` keeps waiting before the recheck time, returns to `ACTIVE` when due,
  and blocks at the deadline.
- `REQUEST_VERIFY` is the only `ACTIVE` to `VERIFY` transition.
- `VERIFY` evaluates the complete explicit acceptance set.

Retries and no-progress observations are separate budgets. Waiting does not
consume either one. The evaluator does not read a clock: adapters must provide
RFC 3339 `now`, `recheck_at`, and `deadline` values, making replay deterministic.
Owners are finite codes (`operator`, `github`, `gitea`, the three provider
adapters, `radioactive-ralph`, the two review bots, or `external`), not
free-form labels. Returned timestamps are normalized to UTC.

## Acceptance predicates

Version 1 supports these adapter-normalized predicate kinds:

- `command_exit_zero`
- `file_exists`
- `checks_green`
- `review_approved`
- `exact_artifact_match`

The policy names the required predicates; adapters perform the actual command,
filesystem, forge, or artifact observation. The evaluator accepts only boolean
`observed` and `satisfied` results. It never executes arbitrary policy content.
Unknown kinds, unknown result identifiers, duplicate identifiers, missing
acceptance configuration, and unsupported schema versions fail closed.

## Failure and secret boundary

JSON decoding rejects unknown fields and trailing values. Semantic validation
rejects invalid states, actions, identifiers, counters, timestamps, and event
payload combinations. Every such input produces `BLOCKED / MALFORMED_INPUT`.

Decision reasons are finite codes. The request schema deliberately has no
provider-error, shell-output, environment, token, or free-form diagnostic
field, so malformed input and provider secrets cannot be reflected in an
evaluator response. Adapters may retain diagnostics in their own protected
logging boundary, but must never put them into this request.

## Adapter parity canary

[`parity-v1.json`](../../internal/enforcement/testdata/parity-v1.json) is the
shared Claude/Codex/OpenCode canary. It records portable requests and expected
decisions for progress, external waits, acceptance, retry, and budget
exhaustion. The Go test runs every case under all three adapter names. Future
generated adapters should run the same file unchanged before installation.

The malformed canaries additionally prove that unknown fields and unsupported
versions block without echoing a token-shaped sentinel.

## Deliberate installer boundary

This foundation does not alter service installation or environment capture.
Persisting a full login-shell environment would cross this module's no-secret
boundary and is not an acceptable adapter mechanism. Any future shell
inheritance work is blocked on a separate least-privilege design: explicit
non-secret allowlists plus references to the existing secret injector, never a
serialized `env` dump. That change must receive its own tests and review rather
than riding with the policy engine.
