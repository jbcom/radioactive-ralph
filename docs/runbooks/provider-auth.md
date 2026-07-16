---
title: Provider auth + setup
description: Get the claude and codex CLIs authenticated so the runtime can drive them.
---

radioactive-ralph is provider-agnostic. The runtime dispatches
claimed tasks to whichever provider CLI the variant's config binds
to. Both shipped providers — `claude`, `codex` — are external CLIs
that must be installed and authenticated separately.

Gemini shipped as a built-in provider previously but was removed on
2026-06-18, when the Gemini CLI's auth endpoint was deprecated (it
now returns HTTP 410 Gone on every invocation). The declarative
provider path (see
[Declarative provider bindings](/design/declarative-provider-bindings/))
still lets a repo bind a self-hosted, gemini-compatible CLI through
`config.toml` if one becomes available again.

## Claude (Anthropic)

### Install

```sh
# macOS / Linux / WSL2
curl -fsSL https://anthropic.com/install.sh | sh

# Or via npm
npm install -g @anthropic-ai/claude-code
```

### Authenticate

```sh
claude
```

First run prompts you to sign in at `console.anthropic.com`. Session
tokens are cached in your user config under `~/.claude/`.

### Verify

```sh
claude --version
claude -p --input-format stream-json < /dev/null
```

The second command should print a stream-json init frame, then exit
cleanly. If it prompts for auth, the session cache is missing.

### radioactive-ralph stateful binding

Claude supports session resume, so the provider binding for `claude`
is stateful — `internal/provider/claudesession` holds the session
lifecycle across task boundaries.

## Codex (OpenAI)

### Install

```sh
npm install -g @openai/codex
```

### Authenticate

```sh
export OPENAI_API_KEY=sk-proj-...
printenv OPENAI_API_KEY | codex login --with-api-key
codex --version
codex login status
```

### radioactive-ralph stateless binding

Codex is bound stateless in v1 — each turn is independent. If the
OpenAI CLI grows session-resume, the binding can promote to stateful
in a later release.

## Choosing a provider per variant

Provider selection is per-variant, configured in
`.radioactive-ralph/config.toml`:

```toml
[variants.fixit]
provider = "claude"
plan_model = "claude-opus-4-5"
plan_effort = "high"

[variants.grey]
provider = "codex"
```

If no provider is set, the runtime falls back to `claude`. Passing an
unknown provider fails loudly at `service start`.

## Verify end-to-end

After both are installed and authenticated:

```sh
radioactive_ralph doctor
```

Expected:

```
[OK] claude       — claude X.Y.Z
[OK] codex        — codex X.Y.Z
```

If any provider is missing but you don't plan to use it, that's fine
— only the providers that appear in your `config.toml` need to
authenticate.
