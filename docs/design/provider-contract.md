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

## Stateful vs. stateless

| Provider | State model | Binding |
|----------|-------------|---------|
| `claude` | Stateful — session resume via `claude --resume <id>` | `internal/provider/claudesession` holds the session lifecycle |
| `codex`  | Stateless — each turn is independent | `internal/provider/codex.go` |
| `opencode` | Stateless in v1 | `internal/provider/opencode.go` |

A stateful binding threads `Result.SessionID` into the next `Request` so
the provider reuses its own conversation context; stateless bindings
ignore it.

## Resolution and validation

`ResolveBinding` picks a provider by name, falling back to the built-in
capability record for `claude`/`codex`/`opencode` when no explicit
override is configured, and defaulting to `claude` when nothing is
specified. `NewRunner` maps the resolved binding's `Type` to a concrete
`Runner` implementation. An unknown provider type fails loudly rather
than silently defaulting.

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
