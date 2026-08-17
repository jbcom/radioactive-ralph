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
mismatch, or rejected verification fails closed with static output and exit
code 2; input and environment values are never echoed.

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
the parity/canary fixtures, and only then enable it. OpenCode's `session.idle`
notification is later than Claude/Codex's synchronous Stop hook. Its generated
plugin polls only the finite `verification_started`/`verification_pending`
states and emits a static progress line every two seconds while it waits; that
keeps Ralph's provider stall lease alive without exposing hook output. A failed,
malformed, or unavailable verdict throws immediately. Its twelve-minute hard
cap leaves two minutes of transport/scheduling grace beyond Ralph's fixed
ten-minute verification budget without allowing an infinite wait. Ralph's
supervisor/reaper remains the load-bearing recovery authority.
