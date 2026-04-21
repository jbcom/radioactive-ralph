---
title: Provider auth + setup
description: Get claude, codex, and gemini CLIs authenticated so the runtime can drive them.
---

radioactive-ralph is provider-agnostic. The runtime dispatches
claimed tasks to whichever provider CLI the variant's config binds
to. All three shipped providers — `claude`, `codex`, `gemini` — are
external CLIs that must be installed and authenticated separately.

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

## Gemini (Google)

### Install

```sh
# Install the Google CLI (tracking — verify current name)
gemini --version
```

### Authenticate

```sh
export GOOGLE_API_KEY=AIza...
gemini --version
```

### radioactive-ralph stateless binding

Same as Codex — stateless binding in v1.

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

[variants.red]
provider = "gemini"
```

If no provider is set, the runtime falls back to `claude`. Passing an
unknown provider fails loudly at `service start`.

## Verify end-to-end

After all three are installed and authenticated:

```sh
radioactive_ralph doctor
```

Expected:

```
[OK] claude       — claude X.Y.Z
[OK] codex        — codex X.Y.Z
[OK] gemini       — gemini X.Y.Z
```

If any provider is missing but you don't plan to use it, that's fine
— only the providers that appear in your `config.toml` need to
authenticate.
