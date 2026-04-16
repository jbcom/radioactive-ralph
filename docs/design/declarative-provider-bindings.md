---
title: Declarative provider bindings (post-v1 design)
description: Config-only path for adding a compatible CLI provider without writing a new Go binding.
---

# Declarative provider bindings (post-v1 design)

> **Design doc, not landed.** This describes the direction tracked
> in v1 PRD § 4.5 task 4 — a config-only way to add a new CLI
> provider binding without writing a Go file. v1 ships with three
> hand-written bindings (`claude`, `codex`, `gemini`). Declarative
> bindings are planned for a later release.

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
# One of: stream-json | plain-stdout | last-message-file
type = "stream-json"

# Binary lookup: absolute path, or bare name resolved via $PATH.
bin = "my-cli"

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

# Session-resume: "stateful" means the runner threads SessionID
# through; "stateless" means every turn is a fresh call.
state_model = "stateless"

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
supervisor event log.

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

On `service start`, the runtime expands every declarative binding
against a synthetic `Request` and validates:

- `bin` resolves on `$PATH` (fail loud if missing)
- `args` template references only known tokens (no typos like
  `{modl}`)
- `type` is one of the three supported framings
- `output_file` template, if present, is syntactically valid
- `session_id_regex`, if present, compiles

Any failure here is a hard error with a pointer to the misbehaving
`[providers.<name>]` block. Validation runs before any worker spawn.

## Migration path

v1 keeps the three hand-written bindings in `internal/provider/`.
When declarative bindings land:

1. Add a new `declarative.go` runner that implements `Runner` and
   reads its config from `Binding.Config`.
2. `NewRunner` returns the declarative runner when
   `Binding.Config.Type` is not one of the built-in type names.
3. The three built-ins keep their hand-written runners (they handle
   edge cases — session resume for claude, schema-file for codex —
   that we don't want to relitigate).
4. Docs shift `docs/design/provider-contract.md` § "Extension model"
   to point at declarative as the default path, with "contribute a
   Go binding" as the fallback for CLIs whose framing doesn't match
   the three supported shapes.

## Non-goals

- **Arbitrary Go callbacks** — the declarative binding is config-
  driven; it does not evaluate user-supplied Go code or Lua/JS.
  Callers who need custom post-processing write a Go binding.
- **Multi-turn within one `Runner.Run` call** — the runtime already
  handles multi-turn via the claim loop + session resume. Declarative
  bindings don't try to batch multiple turns into one CLI invocation.
- **Streaming back to the operator** — the supervisor's event log
  shows claim/start/finish, not assistant-token-level streaming.
  Streaming stays inside the runner.

## Open questions

- Do we want a `providers_path` config option pointing at
  `~/.config/radioactive-ralph/providers.d/*.toml` so declarative
  bindings live outside the repo? Probably yes for user-scope
  defaults; deferred to the implementation PR.
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
- v1 PRD tracking: `docs/plans/2026-04-16-v1-remaining-work.prd.md` § 4.5 task 4
