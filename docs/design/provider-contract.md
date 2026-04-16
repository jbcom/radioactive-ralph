---
title: Provider contract
description: How the runtime binds to claude, codex, gemini, and (future) arbitrary CLI providers.
---

# Provider contract

The runtime is **provider-agnostic**. It dispatches claimed tasks to
whatever CLI the variant's config binds to — `claude`, `codex`,
`gemini`, or a future declarative binding. This page documents the
current (v1) contract and the line between what's code-defined vs
config-defined.

## Contract interface

Defined in `internal/provider/provider.go`:

```go
type Runner interface {
    Run(ctx context.Context, binding Binding, req Request) (Result, error)
}

type Request struct {
    WorkingDir   string
    SystemPrompt string
    UserPrompt   string
    OutputSchema string
    Model        variant.Model
    Effort       string
    AllowedTools []string
}

type Result struct {
    SessionID       string
    AssistantOutput string
}
```

One `Runner.Run` call = one "turn." The runtime gives the runner a
fully-resolved request (system prompt, user prompt, working dir,
allowed tools); the runner invokes the provider CLI, captures stdout,
and returns the assistant's output plus an optional session ID for
multi-turn resume.

## Stateful vs stateless providers

v1 draws a hard line:

| Provider | State model | Binding |
|----------|-------------|---------|
| `claude` | **Stateful** — session resume via `claude --resume <id>` | `internal/provider/claudesession` holds the session lifecycle |
| `codex`  | **Stateless** — each turn is independent | `internal/provider/codex.go` |
| `gemini` | **Stateless** — each turn is independent | `internal/provider/gemini.go` |

A stateful binding means the runtime threads `SessionID` through
`Result` → next `Request` so the provider can reuse its conversation
context. Stateless bindings ignore `SessionID`.

For v1, only `claude` is stateful because only the Claude CLI ships
with session-resume today. If Codex / Gemini CLIs grow session
semantics, their bindings can promote to stateful in a later release
without touching the `Runner` interface.

## Code-defined vs config-defined

### Code-defined (binding implementation)

Things that need to know how to invoke a specific CLI:

- **Argv shape** — e.g. claude uses `claude -p --input-format
  stream-json`, gemini uses `gemini chat` with a different flag set.
  Encoded in the binding's Go file.
- **Prompt/output framing** — whether the provider expects
  stream-json, plain stdin, or a structured prompt file.
- **Session resume semantics** — see above.

These must be code because they vary per-CLI and require real
stream-parsing logic that config can't express.

### Config-defined (binding selection)

Things the operator controls via `.radioactive-ralph/config.toml`:

```toml
# Default for all variants
default_provider = "claude"

# Per-provider settings
[providers.claude]
bin = "claude"                        # optional override of $PATH lookup
model_default = "claude-opus-4-5"     # optional
effort_default = "high"               # optional

# Per-variant override
[variants.fixit]
provider = "claude"                   # force claude for fixit
plan_model = "claude-opus-4-5"
plan_effort = "high"

[variants.grey]
provider = "codex"                    # prefer codex for mechanical work
```

Resolution order at claim time:

1. `variants.<name>.provider` (per-variant)
2. `default_provider` (global)
3. Built-in fallback: `claude`

An unknown provider name fails loudly at `service start`.

## Extension model (post-v1)

The near-term future is **declarative provider bindings**. Target
shape (not yet shipped):

```toml
[providers.my-custom-cli]
type         = "stream-json"         # how to frame I/O
bin          = "mycli"
args         = ["chat", "--stream"]  # argv template
model_flag   = "--model"             # how to pass Model
effort_flag  = "--reasoning"         # how to pass Effort
prompt_mode  = "stdin"               # stdin | file | arg
```

When declarative bindings land, any CLI that speaks one of the
supported framing modes becomes usable without writing a new Go file.
Until then, `claude` / `codex` / `gemini` are the three built-ins and
everything else requires a code contribution under `internal/provider/`.

Declarative-binding work is tracked in the v1 PRD § 4.5 task 4.

## Adding a new built-in provider

If you need a new built-in for v1 (before declarative bindings land):

1. Create `internal/provider/<name>.go` with a type that implements
   `Runner`.
2. Register it in `internal/provider/exec.go`'s factory.
3. Add a doctor check in `internal/doctor/checks.go`.
4. Document its state model + argv shape here.

Keep the new file under the 300-LOC limit; prompt templates and
response parsers that need more belong under
`internal/provider/<name>/`.

## Provider ↔ variant decoupling

A variant profile does **not** hard-bind a provider. `internal/variant/*.go`
declares the persona (name, mission, safety floors, tool budget) and
leaves provider selection to config. That's deliberate — the same
variant persona can run through any compatible provider.

The runtime enforces this at `service start` by reading config, then
resolving each variant's binding, then holding the `Runner` handle
through the claim loop. Variants never see the `Runner` type directly.

## Related

- [Plan format](../guides/plan-format.md) — how plans carry
  `variant_hint`, which becomes `Request.SystemPrompt` shape
- [Provider auth](../runbooks/provider-auth.md) — operator-facing
  setup for each built-in
- [Safety floors](../guides/safety-floors.md) — variant-level
  constraints that fire *before* the provider is called
