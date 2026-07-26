---
title: Provider contract
description: The capability-record contract each provider binding implements.
---

# Provider contract

Each provider is a **capability record**, not a persona: what binary to
invoke, how to run it non-interactively, how to read back its structured
result and usage/cost, how it resumes, and whether it natively fans out
subagents. Shipped providers — `claude`, `codex`, `opencode` — each
implement the same `Runner` interface (`internal/provider/provider.go`).

## Contract interface

```go
type Runner interface {
    Run(ctx context.Context, binding Binding, req Request) (Result, error)
}

type Request struct {
    WorkingDir   string
    SystemPrompt string
    UserPrompt   string
    OutputSchema string
    Model        Model
    Effort       string
    AllowedTools []string
}

type Result struct {
    SessionID       string
    AssistantOutput string
    Usage           Usage // token/cost accounting; zero when unreported
}
```

One `Runner.Run` call is one turn. The runtime gives the runner a
fully-resolved request; the runner invokes the provider CLI under its own
pty, captures output, and returns the assistant's output plus an optional
session ID for resume and a best-effort `Usage` (tokens + `CostUSD`). The
orchestrator accumulates `Usage.CostUSD` per provider to enforce spend
caps.

## Capability record

```go
type BindingConfig struct {
    Type, Bin, Binary string
    Args              []string
    SupportsResume        *bool
    NativeFanout          bool
    // ...model/effort tier overrides
}
```

`NativeFanout` is the flag the orchestrator uses to decide whether a
parallel step-group should be delegated to one fan-out-capable agent
invocation rather than spawned as N Ralph-managed workers:

| Provider | NativeFanout | Evidence |
|----------|--------------|----------|
| `claude` | true | `--agents`, `--agent`, `--forward-subagent-text`, `--bg`/`claude agents` — the CLI natively manages subagents |
| `codex` | false (unconfirmed) | `codex exec --help` exposes no subagent/parallel-workflow flag as of the CLI version evaluated |
| `opencode` | true | `opencode run --agent`, `opencode agent create/list` — a native multi-agent surface |

Project config can deliberately choose Ralph-managed fan-out with
`providers = ["claude", "codex", "opencode"]`. In that mode the resolver
round-robins one binding per plan step and overrides `NativeFanout` to
false on the resolved copy. The provider capability record remains
truthful; only the execution policy changes. This makes every worker,
claim, watchdog, evidence record, and failure independently observable.

## Stateful vs. stateless

| Provider | State model | Binding |
|----------|-------------|---------|
| `claude` | Stateful — session resume via `claude --resume <id>` | `internal/provider/claudesession` holds the session lifecycle |
| `codex`  | Stateless — each turn is independent | `internal/provider/codex.go` |
| `opencode` | Stateless in v1 | `internal/provider/opencode.go` |

A stateful binding threads `Result.SessionID` into the next `Request` so
the provider reuses its own conversation context; stateless bindings
ignore it.

### Codex result and failure channels

The Codex binding invokes `codex exec --json --color never`. A clean exit reads
only the temporary `--output-last-message` file as `AssistantOutput`; terminal
events are never mixed into a successful result. The path must remain the same
regular-file identity across `lstat`, a platform no-follow/nonblocking open, and
the opened-file `fstat`; FIFO, symlink/reparse-point, device, and identity-swap
substitutions fail closed. A limited read then enforces the 16 MiB
authoritative-result ceiling even if the regular file grows.

On a nonzero exit, the last-message file is treated as potentially partial and
is not read. The runner transiently inspects exactly two documented JSONL
fields: `type: "error"` → `message`, and `type: "turn.failed"` →
`error.message`. It classifies them into a closed vocabulary of static failure
categories—authentication, model access, quota, rate limit, network, provider
service, invalid request, or generic failure—and deduplicates those constants.
Classification uses normalized whole tokens and phrases, not arbitrary
substrings; its precedence is pinned by tests.
No provider substring or captured value is retained or surfaced. The fixed
categories are bounded to 4 KiB total, with generic failure as the fail-closed
fallback. Diagnostic inspection is separately bounded to 64 frames, 256 KiB
total, and 64 KiB per frame after excluding Agent's normalized trailing record
delimiter. A 65,536-byte JSON frame is therefore still inspected; its appended
newline is not miscounted as provider payload. Crossing a diagnostic budget
replaces the accumulated categories with generic failure and makes the
collector terminal before message classification. Once all eight categories
have been observed, the collector is likewise information-complete and rejects
subsequent diagnostic payloads before doing more work.

`turn.failed` is authoritative process state rather than optional diagnostic
text. Ralph continues recognizing that bounded event after diagnostic
classification is exhausted, and it dominates an exit code of zero and any
last-message file. A failed turn therefore cannot be laundered by a misleading
process status or partial result.

Codex fully retains and validates observational frames through 4 MiB. A
complete object containing a case-sensitive top-level `type` key must put it
first and contain it only once; a reordered or duplicate discriminator fails
closed with a static schema error. A valid object with no exact `type` key, or
with a non-string value, remains non-authoritative pane noise. The
complete-frame walk validates JSON and inspects syntax without copying or
decoding arbitrary values. A `type`-first command/item frame inside this bound
remains observational even if nested text contains terminal-looking literals.

For a line discarded above that threshold, Agent exposes only a 4 KiB prefix
on a separate, unbuffered framing channel. An immediately recognizable
case-sensitive `type: "turn.failed"` remains authoritative. Every other
structured prefix fails closed because an unseen later duplicate or reordered
failure cannot be ruled out. A whitespace-only prefix is inconclusive and also
fails closed; only a first non-JSON-whitespace byte other than `{` positively
proves non-object pane noise and may be ignored. Prefix bytes are never
rendered, prompt-matched, or included in an error.

Raw terminal lines and every other event type are ignored, including
user/prompt, assistant, reasoning, command, and tool events. Independently, the
shared watchdog uses the static reason `interactive prompt detected`; it never
interpolates the observed prompt line into an error for Codex or any other
provider.

### Claude and OpenCode terminal contracts

Claude is verified against CLI 2.1.218. A terminal `type: "result"` frame is a
success only when `subtype` is exactly `success` and `is_error` is false.
`error_max_turns` maps to a fixed maximum-turn error; every other unsuccessful
or unknown result shape fails closed behind a generic static Claude error.
Provider result text never enters an error. A success frame remains provisional
until Claude exits naturally, so a subsequent nonzero exit status still fails
the turn.

OpenCode is verified against CLI 1.18.3. `opencode run --format json` may emit
several model steps before the session becomes idle. Ralph therefore consumes
the process to its natural exit instead of treating the first `step_finish` as
terminal. It concatenates every text frame, sums usage and cost from every
finish, treats `tool-calls` as intermediate, and accepts only a final reason of
`stop` or `length`. A `type: "error"` event, a nonzero exit, invalid aggregate
usage, a missing finish, or another final reason fails without partial output.
The error event is immediately terminal; Ralph does not allow an endless noisy
tail to keep an already-failed run alive.

Claude and OpenCode independently cap the assembled assistant result and their
Ralph-owned structured-evidence tee at 16 MiB each. Crossing either ceiling
records only a static error, terminates and joins the Agent, and returns a zero
`Result`. OpenCode session identifiers share its authoritative result budget.

### Bounded provider output

The transport bounds **retention**, not provider protocol size. One
`MaxOutputRetentionBytes` budget accounts for the fixed 64 KiB read buffer, the
three bounded 4 KiB discarded-record prefix slots, the callback-owned line, a
line awaiting Watch admission, the line currently being assembled, and
transient bounded slice-growth overlap. The prefix slots correspond exactly to
the provider callback, Watch's pending supervisor admission, and readLoop's
next unbuffered handoff. Both output stages are unbuffered, and a `Progress`
signal transfers the original byte slice without converting it to a second
string. The default aggregate budget derives a 1 MiB retained-line threshold;
a hard aggregate ceiling prevents configuration from turning that bound into
an arbitrary allocation. Prefix capture is independent of the retained-line
threshold, including when the first kernel read already exceeds a tiny
threshold.

Retention is separate from cumulative work. `MaxObservedOutputBytes` optionally
counts raw bytes from every underlying PTY read before line assembly,
retention, or discard. Zero deliberately means unlimited for compatibility.
Claude, OpenCode, and Codex each set a 16 MiB ceiling: exactly 16 MiB is allowed,
while the next byte raises the static `ErrObservedOutputTooLarge`, actively
terminates and reaps the process/session, and is surfaced through `Wait` and
provider supervision. This includes partial lines and Codex records discarded
by `DiscardOversizeOutput`, so continuous noise cannot refresh the stall timer
forever without consuming a bounded work budget.

Providers whose result is the line stream use `RejectOversizeOutput`: crossing
the retained-line threshold records a fixed transport error, kills the whole
process group, closes the pty, and reaps the child. Codex is different.
Codex 0.145's `command_execution.command` string is not capped alongside its
1 MiB `aggregated_output`, and `codex exec --help` publishes no JSONL-record
size contract. Ralph therefore uses a measured 4 MiB retained inspection
threshold: Darwin arm64's 1 MiB `ARG_MAX`, plus common worst-case quote/backslash
JSON expansion and event-envelope headroom. This stays below Agent's existing
8 MiB per-line maximum. Records with control-heavy escaping can exceed 4 MiB;
they are continuously drained without ordinary output emission, then fail
safely if their bounded prefix proves or could still become a structured
object. The independent cumulative 16 MiB raw ceiling is unchanged. Codex's
authoritative success result still comes only from `--output-last-message`.

Activity observations are emitted immediately whenever the underlying pty
`Read` returns bytes, including one-byte trickles and bytes in a record already
being discarded. The channel retains one content-free timestamp and coalesces
to the newest read time. Watch computes the stall deadline from that read time,
not from when downstream backpressure finally lets it consume the observation;
an old queued observation therefore cannot grant a fresh timeout. Partial
content never reaches prompt matching or parsers. Codex keeps a ten-second
cold-start floor for aggressively shortened test timeouts; it is not derived
from or presented as a protocol-size bound. The normal three-minute production
timeout is unchanged. A reader that returns `(0, nil)` without progress is
rejected after 100 consecutive reads, matching Go's bounded no-progress
convention; any successful read resets the counter, and cancellation is checked
between empty reads. The failure is the static `ErrOutputRead` and never
includes terminal content.

### Process lifecycle and output ownership

Agent subprocesses use plain `exec.Command`, not `exec.CommandContext`. Ralph
owns the only cancellation path. A lifecycle mutex linearizes natural
`running -> reaping -> finished`, forced
`running -> reaping -> finished`, and unrecoverable
`running -> failed` paths. A kernel observer reports child exit without reaping
it (pidfd/waitid on Linux, `kern.proc.pid` zombie observation on macOS, a
stable process handle on Windows). Natural observation is deliberately
separate from termination ownership: it may claim `cmd.Wait` only after the
kernel proves exit. A successful explicit termination atomically claims its
own `cmd.Wait`, so output backpressure or a broken observer cannot wedge
reclamation. Every observer probe also takes that mutex; once any path owns
`cmd.Wait`, no raw PID probe can begin and observe a recycled identity.

`Kill` checks the same non-reaping observer under the lifecycle mutex. A late
cancellation handed an already-exited child preserves its natural status.
Otherwise Ralph signals the process group. A real group-signal failure falls
back to the stable direct `os.Process` handle: successful direct termination is
reaped but returns `ErrProcessSessionCleanup`, distinguishing unproven
same-session descendant cleanup from the direct child's outcome.
`ErrProcessTreeCleanup` remains as a compatibility alias. A direct termination
error is re-probed and retried through the same stable handle at most three
times. A transient first failure can converge; a persistent failure moves the
lifecycle to an explicit failed terminal path, releases every Agent-owned
goroutine, and returns `ErrProcessTermination` without claiming the still-live
process was reclaimed.

The final natural/forced classification comes from the status actually returned
by `cmd.Wait`, not merely from a requested signal. If a child exits normally in
the probe-to-signal gap, its real exit status remains natural. On Windows,
`Process.Kill` returning `os.ErrProcessDone` explicitly transfers to natural
reaping rather than marking the earlier termination request as forced.

On Linux, cleanup enumerates the original PTY session, opens and revalidates a
pidfd for each regrouped member, signals it, and repeats with a fixed bound until
no live member remains. This reclaims descendants that use `setpgrp(2)` without
signalling a recycled PID. macOS has no equivalent stable descendant handle:
Ralph detects a live regrouped same-session process and returns
`ErrProcessSessionCleanup` instead of risking an unrelated process. A descendant
that deliberately calls `setsid(2)` is outside the portable original-session
boundary; it cannot wedge Ralph, but full kernel containment remains future
cgroup-v2 work on Linux and Job Object work on native Windows.

Observer backend errors are not retried forever. They are wrapped by
`ErrProcessExitObservation`, followed by explicit termination and independent
reaping convergence. If that termination works, the turn fails honestly and
all resources are reclaimed; if it does not, the joined termination error
still releases Ralph's control path. Hosts outside the release matrix
(currently AIX, DragonFly BSD, FreeBSD, NetBSD, OpenBSD, and Solaris) are
rejected by `Start` until they have an equally strong observer.

PTY EOF means only that output ended. It never starts a timer, forces the
process, or authorizes `cmd.Wait`; a child may close all stdio, work for another
250 ms, and exit naturally with its exact status preserved. The reader uses
nonblocking readiness polling so even an impossible direct-kill failure can
interrupt it. Once process control reaches a terminal state it drains bytes
already ready in the kernel, then stops instead of waiting on a PTY slave
inherited by an out-of-session descendant. Because `O_NONBLOCK` applies to both
sides of the PTY,
`WriteInput` provides a full-write contract with short-write/EAGAIN polling,
caller-cancellation, and terminal-result checks.

`Output` is deliberately unbuffered and lossless during normal supervision.
It closes immediately before `Done`, but only after natural reaping or an
explicit terminal-control result. `Wait` idempotently abandons unread output
and joins every Agent-owned goroutine. Provider supervision drains normal
output, uses `Wait` for natural exit, and synchronously calls
`TerminateAndWait` for prompts, stalls, cancellation, output failures, and
parsed terminal frames. Primary and cleanup failures are joined; a terminal
frame becomes success only after reclamation succeeds.

## Resolution and validation

`ResolveBinding` picks a provider by name, falling back to the built-in
capability record for `claude`/`codex`/`opencode` when no explicit
override is configured, and defaulting to `claude` when nothing is
specified. The supervisor's store-backed resolver accepts either the
backward-compatible singular `provider` key or the plural `providers`
pool described above. `NewRunner` maps the resolved binding's `Type` to
a concrete `Runner` implementation. An unknown provider type fails
loudly rather than silently defaulting.

Only a shipped binary name (`claude`, `codex`, `opencode`) may be named
by a binding sourced from shared config; any other binary — a custom
declarative CLI, an absolute path, a wrapper — must come from an
operator-local override, never from something another party could hand
the supervisor. `agy`/Antigravity was evaluated and found to route
through a cloud control surface (`cloudcode-pa.googleapis.com`), so no
runner is registered for it — see [Declarative provider
bindings](./declarative-provider-bindings.md) for CLIs that don't ship a
hand-written Go runner.

## Adding a new built-in provider

1. Create `internal/provider/<name>.go` implementing `Runner`.
2. Register it in `NewRunner`'s switch and `builtInProvider`.
3. Add its capability record (`default<Name>Provider`) with evidence for
   `NativeFanout`.
4. Add a doctor check in `internal/doctor/checks.go`.
5. Document its state model + argv shape here.

## Related

- [Declarative provider bindings](./declarative-provider-bindings.md) —
  config-only onboarding for compatible CLI framings
- [Provider auth](../runbooks/provider-auth.md) — operator-facing setup
  for each built-in
- [Safety floors](../guides/safety-floors.md) — the never-block invariant
  and spend caps that constrain every provider turn
