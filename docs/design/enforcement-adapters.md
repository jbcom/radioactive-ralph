---
title: Generated enforcement adapters
description: Secret-blind Claude, Codex, and OpenCode hook adapters backed by supervisor-owned verification.
---

# Generated enforcement adapters

The canonical evaluator in `internal/enforcement` is provider-neutral. The
`internal/adapters` package is the narrow provider edge: it renders current
Claude command hooks, Codex command hooks, and an OpenCode plugin, and every
generated command invokes one absolute installed `radioactive_ralph` path.
Adapters normalize provider payloads locally and send only this finite tuple to
the supervisor:

- adapter: `claude`, `codex`, or `opencode`
- event: `PostToolUse` or `Stop`
- Ralph's opaque managed session id

Tool arguments, tool results, transcripts, prompts, environment snapshots, and
provider credentials never cross the hook IPC boundary. An ordinary session
without `RALPH_MANAGED_SESSION_ID` is a no-op and does not even read hook stdin.
A provider launch that is not managed also strips stale inherited Ralph hook
coordinates, so a contaminated service environment cannot accidentally enroll
an ordinary turn. A launch with only one of the managed session or endpoint
coordinates is rejected before the provider subprocess starts.
A managed malformed event, missing executable, missing supervisor, protocol
mismatch, or rejected verification fails closed with static output. Claude
uses its exit-2/plain-stderr block contract, Codex uses its
exit-0/structured-JSON decision contract, and the Ralph-managed OpenCode
launcher uses a finite JSON status plus exit 2. Input and environment values
are never echoed.

## Completion authority

`PostToolUse` refreshes the live worker/session heartbeat. `Stop` resolves the
opaque session to its current owner-guarded store claim. The first Stop writes a
finite `pending` verdict, starts orchestrator-owned verification, and blocks the
Stop immediately; it never parks the provider's hook process behind a long test
suite. A later Stop is allowed only after every task in the turn has a durable
`passed` verdict. `PostToolUse` deletes prior verdicts, so checkout progress
always forces a fresh check. Pending work is deduplicated in-process and safely
restarted after a supervisor restart.

The hook check does not mark a task done. After the provider exits, bounded
authoritative evidence returns through the normal runner and
`VerifyAndCompleteAs` repeats the check and records the verdict. That second
check closes the hook-to-process-exit race. A native-fanout turn must pass every
assigned task.

Adapter v1 manages only tasks with explicit mechanical acceptance. A
malformed acceptance document, an empty object, or an object without a
non-empty command or file predicate is not mechanical acceptance and cannot
enable the hook gate. A judgment-only task needs bounded assistant evidence
that is not available at
the common secret-blind hook boundary, so the orchestrator deliberately omits
the managed marker for that turn. Treating `Stop`, process exit, or hook success
as judgment evidence would violate the completion contract; trapping the turn
in an infinite Stop loop would violate the never-block contract. A mixed native
fanout is therefore entirely unmanaged rather than partially gated.

## Installation boundary

`radioactive_ralph adapters install --target <directory>` builds a
content-addressed release and atomically switches `<directory>/current`. The
bundle contains:

- `bin/radioactive_ralph`
- `claude-hooks.json`
- `codex-hooks.toml`
- `opencode-plugin.js`
- `manifest.json`

It also owns a private isolated runtime home and configuration root for managed
OpenCode launches. OpenCode may bootstrap package/cache state there, but Ralph
rejects every documented config, plugin, command, agent, mode, skill, and
compatible global-skill discovery entry point before each launch. Ralph disables
project config and supplies exactly the reviewed generated plugin through the
finite environment config; ordinary unmanaged OpenCode runs remain transparent.

The installer writes files under a private directory, syncs them before
publication, verifies an existing content-addressed release byte-for-byte
through no-follow regular-file handles, and keeps prior releases for rollback.
Generated commands point to
`<directory>/current/bin/radioactive_ralph`, not a bare command on inherited
`PATH`. This directly prevents the service/minimal-environment failure
`PostToolUse hook exited with code 127`.

Installing a bundle does **not** edit live Claude, Codex, or OpenCode user
configuration. Deployment must first feature-probe the installed provider
versions, merge the reviewed fragment without deleting unrelated hooks, replay
the parity/canary fixtures, and only then enable it. OpenCode's plugin API does
not provide process-level completion authority: `session.idle` is a
notification, and OpenCode 1.18.18 can log a rejected plugin hook while
`opencode run` still exits zero. The generated plugin therefore reports only
`PostToolUse` progress. For a managed turn Ralph starts the real OpenCode CLI
through an absolute, content-verified launcher, preserves any genuine provider
nonzero or signal exit, and only after provider success synchronously submits
the finite `Stop` event. Unavailable, pending, malformed, or failed verification
returns the static OpenCode JSON protocol and exit 2. No-tool runs take this
same launcher finalization path, and sanitized `PATH` cannot produce the former
bare-hook code-127 failure. Ralph's supervisor/reaper remains the load-bearing
recovery authority.

The Linux CI leg also re-applies the guarded versioning, strict-decoding,
coordinate, acceptance, ownership, live-binding, deduplication, and progress
invalidation defects one at a time in isolated source trees. Each mutation
must land at its exact source line, compile, and make its named regression test
fail for the expected assertion before the ordinary suite can be considered
meaningful.
