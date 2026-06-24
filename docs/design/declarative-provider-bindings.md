---
title: Declarative provider bindings
description: Config-only path for adding a compatible CLI provider without writing a new Go binding.
---

# Declarative provider bindings

radioactive-ralph supports config-only provider bindings for CLIs
that fit one of the supported I/O framings. The built-ins
(`claude`, `codex`, `gemini`) still use hand-written Go runners for
their edge cases, but additional compatible CLIs no longer require a
new `internal/provider/<name>.go` file.

## Why declarative bindings

The current shape requires `internal/provider/<name>.go` + a factory
entry for every new provider. That's fine while the list is three
and growing slowly, but it pushes every CLI-compatibility effort
through a code review cycle. Operators who want to try a new CLI —
Claude Desktop's `claude mcp` sessions, a local `llama-cli`, a
corporate-internal chat tool — can't do it without opening a PR here.

A declarative binding says: *if your CLI speaks one of a fixed set of
I/O framings and takes its inputs through a parameterizable argv
template, you can register it in `config.toml` and be done.*

## Proposed config shape

```toml
[providers.my-cli]
# One of: stream-json | plain-stdout | last-message-file.
type = "stream-json"

# Binary lookup: absolute path, or bare name resolved via $PATH.
binary = "my-cli"

# Argv template. Tokens in {braces} are substituted at runtime.
# Available tokens:
#   {model}         resolved Model string (or "" if none)
#   {effort}        resolved Effort string (or "")
#   {prompt_file}   path to a tempfile holding the combined prompt
#   {schema_file}   path to OutputSchema tempfile (or "" if none)
#   {working_dir}   Request.WorkingDir
#   {allowed_tools} comma-joined list of AllowedTools
args = [
  "chat",
  "--stream",
  "--model={model}",
  "--system-from-file={prompt_file}",
  "--cwd={working_dir}",
]

# For types that produce output on a file (last-message-file), where
# to read the final assistant message from.
output_file = "{working_dir}/.my-cli/last.txt"

# Timeouts and retries at the declarative layer. Omit to inherit
# from the global defaults in internal/provider.
turn_timeout = "10m"
max_retries = 2

# Optional: a session-id extractor. For stateful bindings the runner
# needs to know how to pull the session id out of the output. A
# regex against the final frame / output is enough for most CLIs.
session_id_regex = "session=([a-z0-9-]+)"
```

## The three supported framings

### `stream-json`

The CLI writes newline-delimited JSON frames on stdout, each with a
`type` field (`user`, `assistant`, `result`, etc.). The runner parses
frames as they arrive and returns the last `assistant.text`.

This is the claude CLI framing and is the richest option — it lets
the runner surface partial progress, tool-use intents, etc., to the
repo-service event log.

### `plain-stdout`

The CLI prints the assistant's response on stdout, nothing else. If
there's a prelude (warnings, progress), the runner extracts the
JSON block via `{...}` matching when `OutputSchema` is set, or
returns stdout verbatim.

This is the gemini shape.

### `last-message-file`

The CLI writes its final response to a file. The runner reads that
file after the process exits.

This is the codex shape with `--output-last-message`.

## Validation at load time

On `service start`, the runtime resolves provider bindings for every
built-in variant. Any selected declarative binding is validated before
workers can spawn:

- `bin` resolves on `$PATH` (fail loud if missing)
- `args` template references only known tokens (no typos like
  `{modl}`)
- `type` is one of the three supported framings
- `output_file` template, if present, is syntactically valid
- `session_id_regex`, if present, compiles

Any failure here is a hard error with a pointer to the misbehaving
`[providers.<name>]` block. Declarative provider blocks that are
declared but not selected by `default_provider` or a variant override
are left alone.

## Runtime path

The implementation lives in `internal/provider/declarative.go`.
`NewRunner` returns the declarative runner for the three supported
framing types. The built-ins keep their hand-written runners because
they handle provider-specific edge cases — session resume for Claude,
schema-file handling for Codex, and Gemini's current CLI flags.

## Non-goals

- **Arbitrary Go callbacks** — the declarative binding is config-
  driven; it does not evaluate user-supplied Go code or Lua/JS.
  Callers who need custom post-processing write a Go binding.
- **Multi-turn within one `Runner.Run` call** — the runtime already
  handles multi-turn via the claim loop + session resume. Declarative
  bindings don't try to batch multiple turns into one CLI invocation.
- **Streaming back to the operator** — the repo-service event log
  shows claim/start/finish, not assistant-token-level streaming.
  Streaming stays inside the runner.

## Open questions

- Do we want a `providers_path` config option pointing at
  `~/.config/radioactive-ralph/providers.d/*.toml` so declarative
  bindings live outside the repo? Probably yes for user-scope
  defaults, but repo-local config is the implemented path today.
- How do declarative bindings handle `AllowedTools`? Most CLIs
  either always grant all tools or have a bespoke per-provider
  flag. First pass: ignore `AllowedTools` for declarative bindings;
  revisit once a concrete third-party CLI needs it.
- Session-id extraction via regex is crude. A structured
  `session_id_json_path = "$.session.id"` option would be more
  robust for stream-json framings. Consider for the first
  implementation pass.

## Reference

- Current contract: [`provider-contract.md`](./provider-contract.md)
- Go interface: `internal/provider/provider.go::Runner`
- Built-in bindings: `internal/provider/{claude,codex,gemini}.go`
- Implementation: `internal/provider/declarative.go`
