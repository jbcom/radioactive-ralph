---
title: Architecture
updated: 2026-04-14
status: current
domain: technical
---

# Architecture — radioactive-ralph

radioactive-ralph is under architectural rewrite. This page describes the
**target** architecture. See [`docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`](../plans/2026-04-14-radioactive-ralph-rewrite.prq.md)
for the four-milestone rollout plan. Implementation status per component is
tracked in [state](./state.md).

## Critical design constraint

Claude Code has no supported mechanism to inject user-role messages into a
running interactive session from an external process. The only cross-process
channel is the `--input-format stream-json` stdio protocol in headless
(`-p`) mode. Every decision below follows from that.

## Two invocation modes, one daemon engine

| Mode | How you launch | What happens |
|------|----------------|--------------|
| CLI direct | `radioactive_ralph run --variant X` | Terminal runs the pre-flight wizard, launches the supervisor in a detached multiplexer, returns control |
| In-session skill | `/green-ralph` inside a Claude session | Skill runs a Ralphspeak pre-flight wizard, shells out to `radioactive_ralph run --detach`, reports back *"Ralph is playing with his friends"* |

Both modes end up running the same supervisor process with the same variant
profile. The only difference is who asks the pre-flight questions and in
what voice.

## Per-repo config directory

Every repo that uses Ralph has `.radioactive-ralph/` alongside `.git/`:

```text
.radioactive-ralph/
├── config.toml          # committed: variant policy, safety floors, workspace defaults
├── .gitignore           # committed: excludes local.toml
└── local.toml           # gitignored: operator-local overrides (multiplexer pref, etc.)
```

`radioactive_ralph init` creates this tree and appends `.radioactive-ralph/local.toml` to
the repo's root `.gitignore`. Missing `config.toml` = refuse to run with an
in-voice nudge.

## XDG state directory

Heavy, transient, non-portable state lives at
`$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/`, via
`platformdirs.user_state_dir("radioactive-ralph")`. Per-repo per-machine.

```text
$XDG_STATE_HOME/radioactive-ralph/
└── <repo-hash>/                   # sha256(abspath(operator repo))[:16]
    ├── mirror.git/                # only if any variant uses mirror-* isolation
    ├── shallow/                   # only if any variant uses shallow isolation
    ├── worktrees/
    │   └── <variant>-<n>/         # parallel worktrees, per-variant namespaced
    ├── state.db                   # SQLite + sqlite-vec event log
    └── sessions/
        ├── <variant>.sock         # per-variant Unix socket for IPC
        ├── <variant>.pid          # supervisor PID + lock
        ├── <variant>.alive        # mtime heartbeat
        └── <variant>.log          # supervisor stdout/stderr
```

## Four orthogonal workspace knobs

Every variant declares defaults for four dimensions; each can be overridden in
`config.toml`, subject to variant safety floors.

| Knob | Values | Purpose |
|------|--------|---------|
| `isolation` | `shared`, `shallow`, `mirror-single`, `mirror-pool` | Where the work happens |
| `object_store` | `reference`, `full` | Share operator's `.git/objects` or clone independently |
| `sync_source` | `local`, `origin`, `both` | Where the mirror fetches from |
| `lfs_mode` | `full`, `on-demand`, `pointers-only`, `excluded` | How LFS-tracked content is handled |

See the [variants index](../variants/index.md) for the current variant list.
In M3 this is replaced by an auto-generated `variants-matrix.md` sourced
directly from the `Profile` dataclasses.

## Supervisor lifecycle

1. **Pre-flight wizard** — universal checks (clean tree, default branch, `gh`
   auth, `claude` version, multiplexer) + variant-specific questions
   (confirmation gates, budget caps, risky-ops consent). Rendered in plain
   prompts via `rich` for CLI or in Ralphspeak via templates for skill mode.
2. **Multiplexer probe** — `tmux` → `screen` → stdlib `setsid` + double-fork
   fallback. Supervisor runs as a detached child.
3. **Supervisor boot** — acquire `<variant>.pid` lock, open SQLite in WAL,
   bind Unix socket, load variant profile, replay event log, protocol-ping
   a throwaway `claude -p` to verify stream-json parses, initialize the
   `WorkspaceManager`, spawn the session pool.
4. **Event loop** — read stream-json events from each managed session,
   append to SQLite event log (both parsed and raw payload for protocol-drift
   resilience), act on results (commit, open PR, enqueue follow-up). On
   subprocess exit, classify and either resume (`claude -p --resume <uuid>`)
   or finalize.
5. **IPC** — Unix socket serves `radioactive_ralph status`, `radioactive_ralph attach`, `ralph enqueue`,
   `radioactive_ralph stop` commands from sibling processes.
6. **Termination** — per variant policy. Drain events, close socket, remove
   PID, clean worktrees per variant rule, exit.

## Managed-session strategy

Every daemon-owned Claude subprocess:

```bash
claude -p --bare \
  --input-format stream-json \
  --output-format stream-json \
  --include-partial-messages --verbose \
  --permission-mode acceptEdits \
  --allowedTools <variant.tool_allowlist> \
  --model <variant.model_for(stage)> \
  --session-id <stable-uuid> \
  --append-system-prompt <variant-system-prompt>
```

The supervisor pins a stable UUID per session. Resume reuses the same UUID.
Permission mode is `acceptEdits` by default; `bypassPermissions` gated for
destructive variants (old-man, world-breaker).

## Safety floors

Variants declare floors that cannot be weakened by config, env, or a single
CLI flag. Examples:

| Variant | Floor | Override |
|---------|-------|----------|
| old-man | `object_store = full`; refuses default branch; fresh `--confirm-no-mercy` per run | Two CLI flags + config |
| world-breaker | `object_store = full`; fresh `--confirm-burn-everything` per run; spend cap | Two flags + spend cap |
| savage | Spend cap required; fresh `--confirm-burn-budget` per run | Operator raises cap with flag |
| any `shared` isolation | Tool allowlist must exclude `Edit` + `Write` | Impossible (compile-time check) |

## Ralph voice

Ten variant "voices" drive pre-flight questions, status output, attach-stream
events, and shutdown messages. Each variant has its own template library
keyed by `(variant, event_type)`. The voice is the same whether you're seeing
it in the CLI or through a skill — the registry is shared.

## Risk mitigations at a glance

- **Stream-json drift** — `claude` version range pinned in `doctor`; protocol-ping
  on supervisor boot; Pydantic `extra=allow` + raw-event storage.
- **Session resume drift** — sentinel re-prompt after every resume with task-ID
  verification; task-state checkpoints sufficient to reseed a fresh session.
- **Shared-object corruption** — detect broken refs, `git repack -a -d` in
  mirror, retry; destructive variants default to `full` object store.
- **Multiplexer macOS quirks** — tmux strongly recommended; stdlib setsid
  fallback rather than relying on `python-daemon`.
- **Scope creep** — public API = CLI + `Profile` extension point only.

See the [PRD](../plans/2026-04-14-radioactive-ralph-rewrite.prq.md) for the
full risk register.
