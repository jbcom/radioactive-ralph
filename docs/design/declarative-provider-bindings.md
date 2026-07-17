---
title: Declarative provider bindings
description: Config-only path for adding a compatible CLI provider without writing a new Go binding.
---

# Declarative provider bindings

The shipped providers (`claude`, `codex`, `opencode`) use hand-written Go
runners for their edge cases, but a CLI that fits one of a fixed set of
I/O framings can be registered without a new
`internal/provider/<name>.go` file (`internal/provider/declarative.go`).

## The three supported framings

### `stream-json`

The CLI writes newline-delimited JSON frames on stdout, each with a
`type` field (`user`, `assistant`, `result`, etc.). The runner parses
frames as they arrive and returns the last assistant text. This is the
`claude` CLI's framing and the richest option.

### `plain-stdout`

The CLI prints the assistant's response on stdout, nothing else. The
runner extracts a JSON block via brace-matching when an output schema is
set, or returns stdout verbatim.

### `last-message-file`

The CLI writes its final response to a file. The runner reads that file
after the process exits. This is the `codex` shape with
`--output-last-message`.

## Binding shape

```go
type BindingConfig struct {
    Type       string   // stream-json | plain-stdout | last-message-file
    Binary     string   // absolute path, or bare name resolved via $PATH
    Args       []string // argv template
    OutputFile string   // for last-message-file: where to read the result
    SessionIDRegex string // optional session-id extractor for stateful CLIs
    TurnTimeout string
    MaxRetries  int
}
```

Argv template tokens available at dispatch time: `{model}`, `{effort}`,
`{prompt_file}`, `{schema_file}`, `{working_dir}`, `{allowed_tools}`.

## Validation

Before a declarative binding can be dispatched to, the runtime validates:

- the binary resolves on `$PATH` (or the configured absolute path)
- `args` reference only known tokens
- `Type` is one of the three supported framings
- `SessionIDRegex`, if present, compiles

Any failure is a hard error naming the misbehaving binding, not a silent
fallback.

## Non-goals

- **Arbitrary Go callbacks.** The declarative binding is config-driven;
  it does not evaluate user-supplied Go, Lua, or JS. Callers who need
  custom post-processing write a Go binding.
- **Multi-turn batching within one `Runner.Run` call.** Multi-turn
  already happens via the claim loop plus session resume.
- **Token-level streaming to the operator.** The event log shows
  claim/start/finish, not assistant-token-level streaming.

## Reference

- Current contract: [`provider-contract.md`](./provider-contract.md)
- Go interface: `internal/provider/provider.go::Runner`
- Built-in bindings: `internal/provider/{claude,codex,opencode}.go`
- Implementation: `internal/provider/declarative.go`
