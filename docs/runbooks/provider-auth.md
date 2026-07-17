---
title: Provider auth + setup
description: Get the claude, codex, and opencode CLIs authenticated so the supervisor can drive them.
lastUpdated: 2026-07-16
---

The supervisor dispatches work to whichever provider CLI is configured.
All three shipped providers — `claude`, `codex`, `opencode` — are
external CLIs that must be installed and authenticated separately, and
each is local-only: the CLI owns its own agent loop and tool execution on
this machine, even when it calls a hosted model for inference.

`gemini` shipped as a built-in provider previously but was removed on
2026-06-18, when the Gemini CLI's auth endpoint was deprecated (it now
returns HTTP 410 Gone on every invocation). `cursor-agent` is excluded
because it delegates session control to Cursor's cloud rather than
running locally. A self-hosted, gemini-compatible CLI can still be wired
in through the [declarative provider binding
path](../design/declarative-provider-bindings.md).

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
tokens are cached under `~/.claude/`.

### Verify

```sh
claude --version
claude -p --input-format stream-json < /dev/null
```

The second command should print a stream-json init frame, then exit
cleanly. If it prompts for auth, the session cache is missing.

### Binding

Claude supports session resume, so its binding is stateful —
`internal/provider/claudesession` holds the session lifecycle across
turns, and it is the only shipped binding whose capability record
declares `NativeFanout: true` today (the CLI natively manages subagents
via `--agents`/`--agent`/`claude agents`).

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

### Binding

Codex is bound stateless: each turn is independent. Its capability
record's `NativeFanout` is currently `false` (unconfirmed) — no evidence
of a subagent/parallel-workflow flag in `codex exec --help`.

## OpenCode

### Install

Follow the OpenCode CLI's own install instructions for your platform.

### Authenticate

Authenticate however the installed OpenCode CLI documents (typically an
interactive `opencode auth` or equivalent first-run flow).

### Binding

OpenCode's capability record declares `NativeFanout: true` — `opencode
run --agent` and `opencode agent create/list` expose a native multi-agent
surface the orchestrator can delegate a parallel step-group to.

## Verify end-to-end

After the providers you plan to use are installed and authenticated:

```sh
radioactive_ralph doctor
```

Expected:

```
[OK] claude       — claude X.Y.Z
[OK] codex        — codex X.Y.Z
```

Only the providers you actually intend to use need to authenticate — an
unauthenticated or missing provider you never select is not a blocker.
