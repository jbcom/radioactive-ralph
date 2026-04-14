---
title: radioactive-ralph rewrite — per-repo daemon with mirror workspaces
created: 2026-04-14
status: draft
domain: product
---

# Feature: radioactive-ralph — per-repo daemon with mirror workspaces

**Created**: 2026-04-14
**Version**: 3.0 (supersedes v2.0; adopts XDG mirror architecture)
**Timeframe**: 4 sequential PRs, several weeks total

## Priority: P0 (the project does not work today)

---

## Table of contents

1. Overview
2. Critical constraint — why this architecture
3. Architecture
4. Core concepts
   - Per-repo config directory
   - XDG state directory
   - Four workspace knobs
   - Variant-default matrix
   - Safety floors
   - Pre-flight wizard
   - Daemon lifecycle
   - Managed-session strategy
   - Skills as entry points
5. Tasks (M1 → M4)
6. Acceptance criteria
7. Technical notes
8. Risks
9. Out of scope

---

## 1. Overview

radioactive-ralph today is a pair of products wearing the same name:

1. **Ten Claude Code slash-command skills** (`/green-ralph`, `/red-ralph`, etc.) — well-documented, behaviorally distinct, functional inside a Claude session.
2. **A Python daemon** (`ralph run`) — advertised as the durable outer loop but broken in almost every meaningful way: calls `claude` with a non-existent `--message --yes` flag, implements message templates for only 2 of 10 variants, exposes 7 CLI commands in docs that don't exist in code, carries a dead HTTP client.

This PRD rewrites the daemon into a **per-repo meta-orchestrator** that keeps a fleet of Claude subprocesses alive, focused, and productive across days of autonomous work. The daemon is the "Ralph of Ralphs" — it runs either directly from the CLI or is launched in the background by a skill, and in both cases it owns N managed `claude -p` subprocesses (one per worktree), talks to them via stream-json stdin/stdout, and resumes them when they die.

The ten variants stop being just markdown files. They become **behavior profiles** — Pydantic dataclasses declaring parallelism, models, commit cadence, termination policy, tool allowlist, safety floors, and four orthogonal *workspace knobs* (isolation, object store, sync source, LFS handling).

The skills become thin in-session entry points: `/green-ralph` triggers a Ralphspeak pre-flight wizard, launches the daemon in a detached multiplexer (tmux → screen → python-daemon fallback), and returns control to the outer Claude with *"Ralph is playing with his friends."*

---

## 2. Critical constraint — why this architecture

**Claude Code has no supported mechanism to inject user-role messages into a running interactive session from an external process.** Confirmed via research of Claude Code 2.1.89+ and Claude Agent SDK docs (2026-04-14). The only cross-process channel is the `--input-format stream-json` stdio protocol in headless (`-p`) mode.

Consequences:

1. The daemon **never** manages an "outer" Claude session. Sessions the daemon owns are always `claude -p` subprocesses it spawned.
2. A skill running inside an operator's Claude session cannot stay "in touch" with a background daemon via MCP or any other bridge. It launches the daemon and hands off.
3. Resume works via `claude -p --resume <uuid>` reading `~/.claude/projects/<encoded-cwd>/<uuid>.jsonl`. The daemon pins `--session-id <uuid>` on spawn so resume is deterministic.

Earlier PRD versions proposed an MCP server as a live bridge. Abandoned. The skill-launches-detached-daemon shape is the only one that works in Claude Code 2026.

---

## 3. Architecture

```
OPERATOR (~/src/myproject/)
  → runs `ralph init` once per repo → writes .radioactive-ralph/config.toml
  → either `ralph run --variant X` (direct) or `/green-ralph` (in-session)

                 DIRECT CLI                    SKILL WRAPPER
                     │                              │
            ┌────────┴──────────┐         ┌─────────┴────────────┐
            │ pre-flight wizard │         │ Ralphspeak wizard    │
            │ (unless --yes)    │         │ (same registry)      │
            │ spawn daemon      │         │ launch detached      │
            │ attach to logs    │         │ return to outer      │
            └────────┬──────────┘         └─────────┬────────────┘
                     │                              │
                     └────────────┬─────────────────┘
                                  ▼
                   ┌──────────────────────────────┐
                   │   RALPH DAEMON (supervisor)  │
                   │   backgrounded via:          │
                   │     tmux (optimal) →         │
                   │     screen (workable) →      │
                   │     setsid fallback          │
                   │                              │
                   │   Unix socket for IPC        │
                   │   SQLite + sqlite-vec log    │
                   │   Variant profile loaded     │
                   └──────────────┬───────────────┘
                                  │ resolves workspace
                                  ▼
                   ┌──────────────────────────────┐
                   │   WORKSPACE MANAGER          │
                   │   isolation/object_store/    │
                   │   sync_source/lfs_mode       │
                   │                              │
                   │   SHARED → operator's repo   │
                   │   SHALLOW → $XDG/shallow/    │
                   │   MIRROR_* → $XDG/mirror.git │
                   └──────────────┬───────────────┘
                                  │ spawns + manages
                                  ▼
                   ┌──────────────────────────────┐
                   │  MANAGED CLAUDE SUBPROCESSES │
                   │  claude -p --input-format    │
                   │  stream-json, one per        │
                   │  worktree; daemon pipes      │
                   │  messages in, reads events   │
                   │  out, resumes on death       │
                   └──────────────────────────────┘
                                  ▲
                                  │  ralph attach / ralph status
                                  │  via Unix socket
                                  │
                          OPERATOR (other terminal)
```

XDG state layout (via `platformdirs.user_state_dir("radioactive-ralph")`):

```
$XDG_STATE_HOME/radioactive-ralph/
└── <repo-hash>/                 # sha256(abspath(operator repo))[:16]
    ├── mirror.git/              # only if any variant uses mirror-* isolation
    ├── shallow/                 # only if any variant uses shallow
    ├── worktrees/
    │   ├── green-1/ ... green-6/
    │   └── grey-current/
    ├── state.db                 # SQLite event log (shared across variants)
    ├── state.db-wal
    ├── sessions/
    │   ├── green.sock           # per-variant IPC
    │   ├── green.pid
    │   ├── green.log
    │   ├── green.alive          # heartbeat mtime
    │   └── ...
    └── logs/<variant>/<date>.log
```

Operator's repo stays clean — only `.radioactive-ralph/config.toml` and `.radioactive-ralph/.gitignore` are committed; `.radioactive-ralph/local.toml` is gitignored.

---

## 4. Core concepts

### 4.1 Per-repo config directory

```
~/src/myproject/.radioactive-ralph/
├── config.toml          # committed: variant policy, floors, workspace defaults
├── .gitignore           # committed: excludes local.toml
└── local.toml           # gitignored: XDG path, multiplexer pref, operator overrides
```

`ralph init` creates this tree, writes both TOML files, appends `.radioactive-ralph/local.toml` to the repo's root `.gitignore` (not the inner one — we want teammates who clone to be forced through `ralph init --local-only` rather than inheriting the originator's paths).

**Config is the gate.** No `config.toml`, no `ralph run`. Skill refuses with in-voice nudge: *"Ralph hasn't settled in here yet. Run `ralph init` first."*

### 4.2 XDG state directory

All heavy, transient, non-portable state lives in `$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/`. Per-repo per-machine. Operator can blow it away any time to reset — no damage to their working repo.

**Repo hash**: `hashlib.sha256(str(Path.resolve(operator_repo_root)).encode()).hexdigest()[:16]`. Stable per-repo per-machine. Same repo cloned to two locations = two independent mirror workspaces (correct).

### 4.3 Four orthogonal workspace knobs

Every variant declares defaults for four workspace dimensions. Operator can override each via `config.toml`, subject to variant safety floors.

#### Knob 1 — Isolation mode

| Value | Meaning |
|---|---|
| `shared` | Runs in operator's actual repo directory. No clone, no mirror, no worktree. Only valid for variants that exclude `Edit` and `Write` from their tool allowlist (enforced floor). |
| `shallow` | `git clone --depth=1 --filter=blob:none --no-checkout` into `$XDG/shallow/`. Cheap isolated view of committed work. No history, no worktree pool. |
| `mirror-single` | `git clone --mirror` into `$XDG/mirror.git` + a single live worktree at a time under `$XDG/worktrees/`. |
| `mirror-pool` | Same mirror, N concurrent worktrees (N = variant's `max_parallel_worktrees`). |

#### Knob 2 — Object store (relevant only for `mirror-*`)

| Value | Meaning |
|---|---|
| `reference` | Mirror borrows objects from operator's `.git/objects` via `git clone --mirror --reference <operator-repo> --dissociate=false`. Fast clone, small disk, coupled to operator's gc. Daemon handles repack-on-corruption. |
| `full` | Independent clone, no sharing. Slower first clone, larger disk, zero coupling. Default for variants that rewrite history. |

#### Knob 3 — Sync source (relevant only for `mirror-*`)

| Value | Meaning |
|---|---|
| `local` | Fetch only from operator's local repo via `file://` remote. Fast; sees all committed work even unpushed. |
| `origin` | Fetch only from real origin. Only sees pushed work. |
| `both` | Default. Two remotes configured (`local` + `origin`). Default fetch from `local`, push to `origin`. |

#### Knob 4 — LFS mode

| Value | Meaning |
|---|---|
| `full` | Clone LFS objects normally. All worktree checkouts pull binary content. Expensive but complete. |
| `on-demand` | Pointers by default; `git lfs pull --include=<path>` per-file when Ralph's task touches them. |
| `pointers-only` | `GIT_LFS_SKIP_SMUDGE=1`, fetch never pulls LFS blobs. Ralph cannot read/modify LFS-tracked files. |
| `excluded` | `lfs.fetchexclude = *`. Ralph refuses tasks that touch LFS paths with clear error. |

LFS settings are ignored entirely if the repo has no `.gitattributes` with `filter=lfs` entries. Detected on init.

#### Precedence

CLI flags > env vars (`RALPH_*`) > `.radioactive-ralph/config.toml` per-variant override > `.radioactive-ralph/config.toml` `[daemon]` default > variant-profile default > hard-coded project defaults.

Safety floors can pin any knob; floors are not weakened by lower layers.

### 4.4 Variant-default matrix

Defaults each variant declares. Operator can override except where noted (see Safety Floors).

| Variant | Isolation | Parallel | Object store | Sync | LFS |
|---|---|---|---|---|---|
| blue | `shared` (default) or `shallow` | 0 | n/a | n/a | `pointers-only` |
| grey | `mirror-single` | 1 | `reference` | `both` | `pointers-only` |
| red | `mirror-pool` | 8 | `reference` | `both` | `on-demand` |
| green | `mirror-pool` | 6 | `reference` | `both` | `on-demand` |
| professor | `mirror-pool` | 4 | `reference` | `both` | `on-demand` |
| joe-fixit | `mirror-single` | 1 | `reference` | `both` | `pointers-only` |
| immortal | `mirror-pool` | 3 | `full` | `both` | `pointers-only` |
| savage | `mirror-pool` | 10 | `reference` | `both` | `on-demand` |
| **old-man** | `mirror-single` | 1 | **`full` (floor)** | `both` | `on-demand` |
| **world-breaker** | `mirror-pool` | 10 | **`full` (floor)** | `both` | `full` |

### 4.5 Safety floors

Declared per variant. Cannot be weakened by config, env, or single-flag CLI.

| Variant | Floor | Override path |
|---|---|---|
| Any with `shared` isolation | Tool allowlist MUST exclude `Edit` and `Write` | Impossible — variant with write tools in `shared` mode refuses to launch |
| old-man | `object_store = "full"`; refuses to run on repo's default branch (detected via `git symbolic-ref refs/remotes/origin/HEAD`); requires fresh `--confirm-no-mercy` per invocation | Object-store override needs `--confirm-no-mercy` AND `--confirm-shared-objects-unsafe` AND `object_store = "reference"` in config. All three. |
| world-breaker | `object_store = "full"`; spend-cap default $100 if not set; requires fresh `--confirm-burn-everything` per invocation | Same two-step + spend cap |
| savage | Spend-cap default $50 if not set; requires fresh `--confirm-burn-budget` per invocation | Operator can raise cap with new CLI flag `--spend-cap-usd N`; cannot remove |

Default-branch detection uses `git symbolic-ref refs/remotes/origin/HEAD` first; falls back to hard-coded `{main, master, trunk, develop, production, release/*}` only if the symbolic-ref query fails.

### 4.6 Pre-flight wizard

One question registry, two presenters (CLI via rich.prompt, skill via Ralphspeak templates). Each question has:

```python
class PreflightQuestion:
    id: str
    severity: Literal["blocking", "warning", "info"]
    detector: Callable[[Context], QuestionOutcome]   # pure; returns SKIP | ASK | FAIL
    cli_prompt: str
    voice_templates: dict[VariantName, str]          # Ralphspeak per variant
    resolutions: list[Resolution]                    # e.g. "branch from here", "abort"
```

Universal questions (run every variant): working tree clean, on default branch, `gh auth status`, `claude --version` within supported range, multiplexer available, config.toml exists.

Variant-specific questions declared in each profile (e.g. old-man's "this rewrites history, are you sure you're on a disposable branch").

**`--yes` flag** skips info + warning; blocking questions still require answer (operator can pre-answer via config.toml defaults). Skill mode always presents — skill wrapper itself is `--yes` from daemon's POV, skill handles interaction.

**Remembered answers**: operator's config.toml records per-variant answers:

```toml
[variants.green]
auto_branch_strategy = "from_current"      # answered once, never asked again
budget_cap_usd = 50
```

### 4.7 Daemon lifecycle

1. **Entry point**: `ralph run --variant X` resolves profile + overrides, runs pre-flight, if OK spawns supervisor via multiplexer.
2. **Multiplexer probe**: `$TMUX` set or `tmux` on PATH → tmux. Else `screen` → screen. Else stdlib `os.setsid()` + double-fork fallback. (Dropping `python-daemon` dep after prototyping — stdlib is enough.)
3. **Supervisor boot** (`ralph _supervisor` internal cmd):
   - Acquires lock on `$XDG/.../sessions/<variant>.pid` (refuses if live).
   - Opens SQLite in WAL, loads sqlite-vec (soft-fails to FTS5 if extension unavailable).
   - Binds Unix socket at `$XDG/.../sessions/<variant>.sock`.
   - Starts heartbeat thread (touches `<variant>.alive` every 10s).
   - Loads variant profile + resolved workspace config.
   - Replays event log → rebuild in-memory state.
   - Protocol-ping: spawn throwaway `claude -p`, send "respond PONG", verify parse. Abort boot on failure.
   - Initializes WorkspaceManager per isolation mode.
   - Starts session pool.
4. **Workspace init (first run)**: WorkspaceManager creates mirror/shallow/worktree as needed. Copies operator's `.git/hooks/` to mirror's `.git/hooks/` (unless `copy_hooks = false`). Detects LFS usage. Applies knobs.
5. **Session pool**: per slot, create/reuse worktree, spawn `claude -p --input-format stream-json --session-id <uuid>` with stage-appropriate system prompt. Record session in SQLite.
6. **Event loop**: read stream-json events, append to log (parsed + raw), act on result events. On subprocess exit, classify (clean | context-exhausted | rate-limited | crashed | operator-killed) and:
   - clean/task-done → mark task complete, pick next
   - context-exhausted → resume with `--resume <uuid>` + sentinel re-prompt ("what task ID are you working on?") for mismatch detection
   - rate-limited → exponential backoff, resume when clear
   - crashed → log, resume once, then escalate to operator if it crashes again
   - operator-killed → graceful drain
7. **IPC**: Unix socket accepts JSON-line commands: `status`, `attach` (stream events), `enqueue <task-json>`, `stop [--graceful]`, `reload-config`.
8. **Termination**: per variant's termination policy. On termination: drain events, close socket, remove PID, clean worktrees (per variant policy: destroy for mirror-single, keep for mirror-pool unless operator says), exit.

### 4.8 Managed-session strategy

Every managed invocation:

```bash
claude -p --bare \
  --input-format stream-json \
  --output-format stream-json \
  --include-partial-messages --verbose \
  --permission-mode <acceptEdits|bypassPermissions> \
  --allowedTools <variant.tool_allowlist> \
  --model <variant.model_for(stage)> \
  --session-id <stable-uuid> \
  --append-system-prompt <path-to-variant-system-prompt>
```

- `--bare` skips project CLAUDE.md / hook / MCP auto-discovery for reproducibility. Variant's system prompt via `--append-system-prompt`.
- `--permission-mode default` NEVER used (daemon has no TTY for interactive prompts).
- `acceptEdits` for normal variants; `bypassPermissions` for old-man/world-breaker behind their gates.

**Spend tracking**: supervisor parses `usage` field from stream-json result events, accumulates `input_tokens`/`output_tokens` per model in SQLite, computes USD from hardcoded-updated pricing table. On cap reached → graceful stop with Ralphspeak message *"Ralph burned his allowance. Going home."*

**Session ID pinning** — daemon supplies `--session-id <uuid>` on every spawn and resume. Avoids Claude Code issue #44607 (interactive mode ID ≠ on-disk filename; issue does not affect `-p` mode).

### 4.9 Skills as entry points

Each `skills/<variant>/SKILL.md` ≤30 lines, body structure:

```markdown
---
name: green-ralph
description: The Classic. Unlimited loop, all repos, sensible model tiering. Launches the radioactive-ralph daemon in the background and returns.
---

1. Check `.radioactive-ralph/config.toml`. If missing, run `ralph init` interactively and wait for operator to commit the result.
2. Run the green-ralph pre-flight checks: working tree, branch policy, `gh`/`claude` auth, multiplexer availability. Speak in Ralph's green-tier voice.
3. For each failure, offer resolutions (branch from here, switch branch, re-auth) and apply the operator's choice.
4. When all checks pass, shell out: `ralph run --variant green --detach`
5. Report in Ralphspeak: "Ralph is playing with his friends on branch `ralph/green-<date>`. Check on him with `ralph status` or `ralph attach`."
```

Rich behavioral lore moves into `VariantProfile.voice` templates and auto-generated `docs/variants/<name>.md` pages.

---

## 5. Tasks

### Milestone 1 — Marketplace fix + aggressive cleanup (this PR, branch `fix/marketplace-structure-and-docs-audit`)

- **M1.T1** — Fix `.claude-plugin/marketplace.json`:
  - Rename marketplace → `jbcom-plugins`
  - Rename plugin → `ralph`
  - Keep `source: "./"` (matches `anthropics/skills`)
  - Set `"strict": false`
  - List skills explicitly under `skills: [...]`
  - Remove non-standard `$schema`
- **M1.T2** — Update or delete `.claude-plugin/plugin.json` (strict=false makes it optional).
- **M1.T3** — Rewrite README install section with correct syntax:
  ```
  claude plugin marketplace add jbcom/radioactive-ralph
  claude plugin install ralph@jbcom-plugins
  ```
- **M1.T4** — Rewrite README "Core commands" — currently 7 nonexistent commands. Show the future-facing CLI (`ralph init/run/status/attach/stop/doctor`), mark each implemented/planned with milestone reference.
- **M1.T5** — Delete the false `claude --print subprocesses` claim. Replace with: "Under rewrite — see `docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`."
- **M1.T6** — Delete dead code: `src/radioactive_ralph/github_client.py` (superseded by `forge/github.py`).
- **M1.T7** — Stub broken implementations with `NotImplementedError("under rewrite, see PRD M2")`:
  - `agent_runner._spawn_agent`
  - `orchestrator.Orchestrator.run`
- **M1.T8** — Prune `cli.py` to the real surface: `status`, `doctor`. `run` raises `NotImplementedError` with PRD pointer. Remove any stubs for dashboard/discover/pr/install-skill if present.
- **M1.T9** — Fix `tests/test_cli.py::test_main_verbose` — currently empty `pass` body. Assert exit status + log level.
- **M1.T10** — Fix `tests/test_orchestrator.py::test_step_spawns_agents` — passes `repo_name` as kwarg but `WorkItem.repo_name` is a computed property. Correct to `repo_path=...` and derive.
- **M1.T11** — Rewrite `docs/reference/architecture.md` to the new architecture. Mark `updated: 2026-04-14`.
- **M1.T12** — Rewrite `docs/guides/design.md` with new UX (`ralph init` then CLI or skill, "Ralph plays with his friends" metaphor, per-repo config + XDG state).
- **M1.T13** — Rewrite `docs/reference/state.md` — what's done (hygiene), what's planned (M2/M3/M4).
- **M1.T14** — Rewrite `docs/reference/testing.md` — preserve unit strategy, add integration plan, `CLAUDE_AUTHENTICATED` gate note.
- **M1.T15** — Update `CLAUDE.md` module map (currently 7 of 16). Remove `pr_manager.py` "gh CLI wrapper" mischaracterization. Add "What radioactive-ralph is NOT" section mirroring this PRD's out-of-scope list.
- **M1.T16** — `CHANGELOG.md` — new `[Unreleased]` entry describing the architectural pivot.
- **M1.T17** — Commit this PRD to `docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`.
- **M1.T18** — `hatch run test`, `hatch fmt --check`, `hatch run hatch-test:type-check`, `hatch run docs:build` all green. Fix any breakage from deletions/stubs.

### Milestone 2 — Daemon skeleton + per-repo config + XDG workspace + session control

- **M2.T1** — `src/radioactive_ralph/init.py`: `ralph init` wizard. Creates `.radioactive-ralph/` tree, writes `config.toml` + `local.toml`, appends `local.toml` to repo root `.gitignore`.
- **M2.T1b** — `src/radioactive_ralph/workspace/modes.py`: `IsolationMode`, `ObjectStoreMode`, `SyncSourceMode`, `LfsMode` enums + `WorkspaceConfig` Pydantic model.
- **M2.T1c** — `src/radioactive_ralph/workspace/manager.py`: `WorkspaceManager`. Dispatches on isolation:
  - `shared` → no-op, cwd is operator repo
  - `shallow` → `git clone --depth=1 --filter=blob:none --no-checkout` into `$XDG/shallow/` + `GIT_LFS_SKIP_SMUDGE=1`
  - `mirror-*` → `git clone --mirror` with `--reference`/`--dissociate=false` flags per `object_store`, two remotes per `sync_source`, hook copy, LFS detection, per-mode LFS config
- **M2.T1d** — Mirror sync manager: per-variant cadence (config-driven), handles `git fetch` + `git lfs fetch --include` on demand.
- **M2.T1e** — Repack-on-corruption: detect broken shared-object refs (missing blob error during worktree checkout), `git repack -a -d` in mirror, retry. Logged in event log.
- **M2.T1f** — LFS detection on init: parse `.gitattributes`, log detected LFS filter paths. If zero LFS files detected, skip all LFS configuration regardless of `lfs_mode`.
- **M2.T2** — `src/radioactive_ralph/config.py` rewrite: Pydantic-settings layered (CLI → env → config.toml per-variant → config.toml [daemon] → variant defaults → project defaults). Per-variant `[variants.<name>]` sections support all four knobs plus remembered pre-flight answers.
- **M2.T2a** — `XDGPaths` helper using `platformdirs.user_state_dir("radioactive-ralph")`, derives repo-hash, builds workspace subdirectory paths.
- **M2.T3** — `src/radioactive_ralph/daemon/db.py`: SQLite + WAL + sqlite-vec loadable extension (graceful fallback to FTS5). Schema under `src/radioactive_ralph/daemon/schema/*.sql`. `EventLog.append(Event)` + `EventLog.replay() -> Iterator[Event]`. `events` table stores both `payload_parsed` (JSON) and `payload_raw` (bytes) for protocol-drift resilience.
- **M2.T3a** — `tasks` table + `sessions` table + `task_vec` virtual table (vec0) for semantic dedup. Task-state checkpoints as distinct event types, rich enough to reseed a fresh session from scratch on resume failure.
- **M2.T4** — `src/radioactive_ralph/daemon/socket_ipc.py`: Unix socket server + client, JSON-line protocol. Commands: `status`, `attach`, `enqueue`, `stop`, `reload-config`.
- **M2.T4a** — Heartbeat: supervisor writes `<variant>.alive` mtime every 10s. `ralph status` treats >30s stale as dead regardless of PID file.
- **M2.T5** — `src/radioactive_ralph/daemon/multiplexer.py`: probe tmux → screen → setsid fallback. `spawn_detached(cmd, log_path, pid_path)`. Session names `ralph-<variant>-<repohash[:8]>`.
- **M2.T5a** — Fallback detach uses stdlib `os.setsid()` + double-fork + `stdin=DEVNULL` + redirect stdout/stderr to log file. No `python-daemon` dependency.
- **M2.T6** — `src/radioactive_ralph/daemon/session.py`: `ClaudeSession` wrapping `claude -p --input-format stream-json`. Methods `send_user_message`, `iter_events`, `interrupt`, `wait_for_idle`, `resume`. Records session in SQLite.
- **M2.T6a** — Protocol-ping handshake in `ClaudeSession.__init__`: send "respond PONG", verify parsed event, abort on mismatch. Integration tested on every CI run with `CLAUDE_AUTHENTICATED`.
- **M2.T6b** — Sentinel re-prompt after resume: daemon sends "what task ID are you currently on?" right after `--resume <uuid>`. On mismatch or no-response, treat as context-lost, reseed task from checkpoint events.
- **M2.T7** — `src/radioactive_ralph/daemon/supervisor.py`: PID lock, socket bind, event replay, session pool, IPC loop, termination.
- **M2.T7a** — Multi-variant coexistence: supervisor names all paths per-variant (`<variant>.sock`, `<variant>.pid`, etc.). SQLite is shared across variants in the same repo — WAL handles single-writer per variant supervisor; cross-variant events interleave cleanly. `ralph status --all` aggregates.
- **M2.T8** — `src/radioactive_ralph/cli.py` commands:
  - `ralph init [--local-only]`
  - `ralph run --variant X [--detach] [--yes] [--spend-cap-usd N]`
  - `ralph status [--variant X | --all]`
  - `ralph attach --variant X`
  - `ralph stop [--variant X | --all] [--graceful]`
  - `ralph doctor`
  - `ralph _supervisor --variant X --repo-root P` (hidden)
- **M2.T9** — Unit tests. ≥90% coverage on `src/radioactive_ralph/daemon/`, ≥85% on config/init/workspace.
- **M2.T10** — Integration test (always-on): `ralph init` in a temp repo → `ralph run --variant blue --detach` (blue is read-only; requires only config.toml and real claude for the protocol-ping, which we skip unless `CLAUDE_AUTHENTICATED`). Assert socket reachable, `ralph status` returns healthy, `ralph stop` clean. No leftover PID/socket/worktree.
- **M2.T10a** — CI matrix: add `macos-14` for integration tests.
- **M2.T11** — Integration test (gated on `CLAUDE_AUTHENTICATED=1`): spawn real `claude -p`, send message, verify response event logged, kill subprocess, resume via session ID, verify continuity + sentinel re-prompt success.

### Milestone 3 — Ten variants + pre-flight wizard + voice

- **M3.T1** — `src/radioactive_ralph/variants/base.py`: `VariantProfile`, `Stage`, `Model`, `TerminationPolicy`, `PreflightQuestion`, `VoiceProfile`, `SafetyFloors` dataclasses. `VariantProfile` is a public extension point — documented for users who want custom variants.
- **M3.T1a** — `ralph run --variant-module X.Y.Z` loads a user-provided module and expects a `VARIANT: VariantProfile`. Documented as trust-on-first-use: importing means executing. Docs warn.
- **M3.T2** — Ten variant files under `src/radioactive_ralph/variants/` (`green.py` ... `world_breaker.py`), each ≤300 LOC, each exporting a frozen `VariantProfile` faithful to the current `skills/*/SKILL.md`.
- **M3.T2a** — Each profile declares all four workspace knobs (isolation, object_store, sync_source, lfs_mode) at their documented defaults.
- **M3.T2b** — `SafetyFloors` sub-structure on each profile. old-man + world-breaker pin `object_store = full`, require two-step override (`--confirm-<variant>` + `--confirm-shared-objects-unsafe` + config). savage requires spend cap.
- **M3.T2c** — First-invocation dry-run mode for risky variants: `old-man`, `world-breaker`, `savage` on their first run in a given repo print a full plan in Ralphspeak and require `ralph run --variant X --yes-ive-read-the-plan` on second invocation. Consent recorded per-repo in `local.toml`.
- **M3.T3** — `src/radioactive_ralph/workspace/worktree.py`: `WorktreeManager` for `mirror-*` isolation modes. Creates/destroys/reconciles worktrees under `$XDG/.../worktrees/`. On boot, reconcile against `git -C mirror.git worktree list --porcelain` — destroy orphans.
- **M3.T3a** — Hook copying: on mirror init, copy operator's `.git/hooks/` (executable files only) to mirror's `.git/hooks/`. Override via `config.toml` (`copy_hooks = false`). Documented.
- **M3.T4** — `src/radioactive_ralph/preflight.py`: question registry + runner. Each question has severity + detector + resolutions. Default-branch detection via `git symbolic-ref refs/remotes/origin/HEAD` with fallback list.
- **M3.T4a** — Remembered-answer logic: operator's answers written to `config.toml` per-variant section; subsequent runs skip those questions unless operator passes `--reask`.
- **M3.T5** — `src/radioactive_ralph/voice.py`: per-variant voice generator. Template library keyed by `(variant, event_type)`. Every user-facing emission (status, events, pre-flight prompts, Ralphspeak handoff lines) is voiced.
- **M3.T5a** — Spend tracking: supervisor parses `usage` events, accumulates per-model, computes USD from pricing table. Cap-hit → graceful stop, voiced as *"Ralph burned his allowance. Going home."* Pricing table is a module-level dict; stale prices are a known risk, documented.
- **M3.T6** — Rewrite all 10 `skills/*/SKILL.md` as thin entry points (<30 lines). Each: (1) check/bootstrap config, (2) run variant's pre-flight, (3) shell out to `ralph run --detach`, (4) return Ralphspeak handoff.
- **M3.T7** — `scripts/generate_variants_matrix.py`: reads every `VariantProfile`, emits `docs/reference/variants-matrix.md`. Committed; CI re-runs and fails on drift.
- **M3.T8** — Rewrite per-variant docs under `docs/variants/*.md` to pull from the profile (auto-generated tables) + hand-written lore sections allowed above/below.
- **M3.T9** — Unit tests:
  - Parameterized test — each profile loads, fields are valid, tool allowlist is subset of known tools, gated variants declare gate.
  - blue profile excludes `Edit`/`Write` from allowlist.
  - Any variant with `shared` isolation excludes `Edit`/`Write` (floor enforced at profile-compile time via validator).
  - savage/old-man/world-breaker declare gates + spend caps.
  - grey is single-pass, max_parallel_worktrees=1.
  - All 10 profiles type-check under `mypy --strict`.
- **M3.T9a** — Safety-floor unit tests: TOML override alone cannot weaken old-man `object_store`. Only `--confirm-no-mercy` + `--confirm-shared-objects-unsafe` + TOML together succeed.
- **M3.T9b** — Config round-trip tests: each variant's defaults round-trip through `config.toml` write → read → compare.
- **M3.T10** — Pre-flight unit tests: each variant's pre-flight with mocked repo state produces expected questions/refusals.

### Milestone 4 — Integration harness + doctor + release

- **M4.T1** — `tests/integration/conftest.py`: tmp git repo fixture, fake origin (`git init --bare` in tmp), optional real `claude -p` if `CLAUDE_AUTHENTICATED` set, fake gh responses via respx.
- **M4.T2** — Scenario: full grey-ralph single-pass end-to-end. `ralph init` → `ralph run --variant grey` → assert PR to fake origin → supervisor exits cleanly → mirror persists, worktrees cleaned.
- **M4.T3** — Scenario: green-ralph partial run with SIGTERM. Spawn, let 2 PRs open, SIGTERM multiplexer, assert graceful drain.
- **M4.T4** — Scenario: session death + resume. Start supervisor, one managed session active, SIGKILL `claude -p` subprocess mid-stream, assert supervisor detects, resumes, sentinel prompt verifies continuity.
- **M4.T5** — Scenario: pre-flight refusal. old-man on default branch without gate → supervisor exits 2 with clear remediation.
- **M4.T5a** — Scenario: safety floor rejections — old-man with TOML `object_store = reference` but only one confirm flag → refuses. With both flags → proceeds.
- **M4.T6** — Scenario: multiplexer fallback. Force PATH without tmux → screen used. Without screen → setsid fallback used. Each case: daemon comes up, socket reachable, status healthy.
- **M4.T6a** — Scenario: `shared` isolation (blue-ralph default) creates no mirror, no worktree. Reads operator repo, posts review comments, exits.
- **M4.T6b** — Scenario: `shallow` isolation creates `$XDG/shallow/`, no mirror machinery, no worktree pool.
- **M4.T6c** — Scenario: `mirror-single` + `reference` object store. Share operator's objects; simulate operator `git gc --aggressive` → Ralph detects broken refs → repacks mirror → resumes.
- **M4.T6d** — Scenario: LFS detection. Repo with `.gitattributes` containing `*.psd filter=lfs`. Blue-ralph pointers-only → never pulls blobs. Green-ralph on-demand → pulls when task touches a `.psd`.
- **M4.T6e** — Scenario: pinned hooks. Mirror init copies operator's `.git/hooks/pre-commit`. Ralph commit triggers the pre-commit hook. Verify hook ran.
- **M4.T7** — `ralph doctor` full rewrite:
  - git version ≥ 2.5
  - `claude --version` within supported range (pinned in code)
  - `gh auth status`
  - tmux OR screen OR Python stdlib sufficient for fallback
  - sqlite3 with loadable-extension support; sqlite-vec loadable (warn-only if missing, FTS5 fallback)
  - `platformdirs.user_state_dir` writable
  - Operator repo has `.radioactive-ralph/config.toml` (warn if absent + offer to run `ralph init`)
  - Exit 0 green; exit 1 with ranked remediation list
- **M4.T7a** — Doctor version-range check: if `claude --version` outside pinned range, exit 1 with exact `npm install -g @anthropic-ai/claude-code@<range>` suggestion.
- **M4.T8** — Docs polish: final architecture diagram in `docs/reference/architecture.md`; UX walkthrough in `docs/guides/design.md`; auto-generated `variants-matrix.md`; extension guide in `docs/guides/custom-variants.md`.
- **M4.T8a** — `docs/guides/custom-variants.md` — how to write your own `VariantProfile` and load via `--variant-module`. Clearly marked trust-on-first-use.
- **M4.T9** — Release 1.0.0 via release-please; publish to PyPI via existing release workflow. Record `assets/demo.gif` of the full `ralph init → ralph run --variant grey → PR opens → ralph stop` flow.

---

## 6. Acceptance criteria

### Milestone 1 — marketplace + hygiene
- `claude plugin marketplace add jbcom/radioactive-ralph && claude plugin install ralph@jbcom-plugins` succeeds post-merge.
- No README claim references a command or flag that doesn't exist.
- `hatch run test` passes (2 broken tests fixed).
- `hatch run docs:build` passes.
- `ruff check` clean; `mypy --strict` clean on remaining source.
- All touched `docs/` pages carry `updated: 2026-04-14`.
- `.radioactive-ralph/` does NOT yet exist in this PR — lands in M2 with `ralph init`.
- `docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md` committed.
- `CHANGELOG.md` `[Unreleased]` section describes the pivot.
- PR description links this PRD.

### Milestone 2 — daemon skeleton
- `ralph init` creates `.radioactive-ralph/` tree correctly in a fresh temp repo, writes both TOML files, appends to root `.gitignore`.
- `ralph run --variant blue --detach` launches a backgrounded supervisor; `ralph status` healthy; `ralph stop` clean.
- `pytest tests/daemon/` ≥90% coverage on daemon/.
- Gated integration test (real claude) passes: spawn, inject, verify, kill, resume, sentinel-verify continuity.
- `.claude-plugin/marketplace.json` validates under `claude plugin validate .`.
- `ralph _supervisor` hidden from `--help`.

### Milestone 3 — variants + workspaces + preflight
- `pytest tests/variants/ -v` — all 10 variants pass parameterized tests.
- `shared`-mode variants' tool allowlist enforced at profile-compile time.
- old-man/world-breaker safety-floor tests pass (single-flag rejected, two-flag accepted).
- grey: single-pass + max_parallel_worktrees=1.
- All variants type-check under `mypy --strict`.
- `scripts/generate_variants_matrix.py` stable across consecutive runs.
- Skills ≤30 lines each.

### Milestone 4 — integration + release
- `pytest tests/integration/` all scenarios pass (gated skip cleanly).
- `ralph doctor` exit 0 healthy, exit 1 with remediation on broken.
- `docs/reference/variants-matrix.md` generated, CI drift check passes.
- 1.0.0 published to PyPI.
- `assets/demo.gif` records end-to-end demo.

---

## 7. Technical notes

**Session ID strategy.** Stable UUID per managed session via `--session-id <uuid>`. Avoids issue #44607 (interactive mode only; `-p` mode is sound).

**sqlite-vec vs FTS5.** Ship with sqlite-vec for embeddings. Graceful degrade to FTS5 if extension loading fails. Never sqlite-vss.

**`--bare` flag.** All daemon-spawned sessions. Skip project CLAUDE.md/hooks/MCP for reproducibility. Variant provides system prompt via `--append-system-prompt`.

**Permission mode.** Daemon sessions never use `default`. `acceptEdits` normal; `bypassPermissions` gated for old-man/world-breaker.

**State directory.** `.radioactive-ralph/` in-repo for config only; `$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/` for everything else. Operator can nuke XDG to reset; zero working-repo impact.

**Concurrency.** SQLite WAL one-writer + N-readers. Per-variant supervisors coexist; shared event log interleaves cleanly. Cross-repo supervisors are independent.

**Default-branch detection.** `git symbolic-ref refs/remotes/origin/HEAD` first. Fallback hard-coded list `{main, master, trunk, develop, production, release/*}`.

**Hook preservation.** Mirror init copies executable files from operator's `.git/hooks/` to mirror's `.git/hooks/`. Override via `copy_hooks = false`.

**LFS handling.** Detected on init via `.gitattributes`. Skipped entirely if no LFS. Otherwise applies variant's `lfs_mode`.

**Multiplexer detection.** `$TMUX` set OR tmux on PATH → tmux. Else screen. Else stdlib setsid + double-fork. No python-daemon dep.

**Spend tracking pricing table.** Hardcoded; stale prices = undercharging/overcharging. Known risk; doctor warns when pricing table is >30 days old (ship date).

**Nested claude.** Managed subprocess may spawn claude if the task requires it. Second-level is the managed session's responsibility, not the daemon's.

**Sandbox.** If Claude Code has strict sandboxing, the skill's Bash launch of `ralph run` needs `$XDG_STATE_HOME/radioactive-ralph/` in `additionalDirectories`. Documented in skill prereqs.

**Default-branch detection is per-remote.** Ralph queries `origin`'s default branch, not `local`'s. Some operator repos have diverging locally-default vs origin-default branches; Ralph trusts origin because that's where PRs go.

**Variant trust-on-first-use.** `--variant-module X.Y.Z` imports arbitrary Python. Users run at own risk. Docs note this.

---

## 8. Risks (consolidated with mitigations)

### R1 — Stream-json protocol drift
Mitigations: pin `claude` version range in doctor; protocol-ping on supervisor boot; Pydantic `extra=allow` + raw-event storage for forward compat; integration test gated on `CLAUDE_AUTHENTICATED`.

### R2 — Session file format drift
Mitigations: sentinel re-prompt after every resume with task-ID verification; task-state checkpoints in own event log sufficient to reseed a fresh session; doctor pins `claude` version; integration resume test on every CI.

### R3 — Pre-flight UX complexity
Mitigations: silent-when-passing detectors; remembered answers in config.toml; `--yes` flag; three severity levels; single registry shared by CLI + skill.

### R4 — Multiplexer fallback quirks on macOS
Mitigations: tmux strongly recommended (doctor gives top-priority remediation); hand-rolled stdlib setsid instead of python-daemon; heartbeat file detects liveness independent of PID; macOS CI matrix.

### R5 — Risky variants run unattended
Mitigations: per-variant safety floors (old-man `object_store=full` pinned; two-step override requires both CLI flag AND explicit config); spend caps for savage/world-breaker; first-invocation dry-run consent; mirror-based isolation means destructive ops stay in XDG-land, never touch operator's working tree.

### R6 — Scope creep
Mitigations: explicit out-of-scope list; public API = CLI + VariantProfile extension point only; no module-level semver promises; `--variant-module` is the extension mechanism, core stays narrow.

### R7 — config.toml-as-gate breaks for teammate who cloned
Mitigations: `local.toml` gitignored, operator-specific; `ralph init --local-only` for existing-config bootstrap; skill/CLI detect missing `local.toml` and prompt.

### R8 — Multi-variant race conditions
Mitigations: per-variant-scoped paths (`<variant>.sock`, `<variant>.pid`, `<variant>-<n>` worktrees); SQLite WAL handles interleaved writes; `ralph status --all` aggregates; `allow_concurrent_variants` config toggle if operator wants exclusivity.

### R9 — Shared-object reference corruption
Mitigations: repack-on-corruption with automatic recovery; integration test simulates operator `git gc --aggressive`; destructive variants default to `full` object store and require two-step override for `reference`.

### R10 — LFS surprises
Mitigations: auto-detect; variant-appropriate defaults; pointers-only for blue/grey/joe-fixit/immortal; operator override per variant; `excluded` mode refuses tasks that touch LFS paths with clear error.

### R11 — Hook skipping
Mitigations: copy operator's `.git/hooks/` to mirror on init; override via `copy_hooks = false`; documented.

### R12 — Spend-tracking pricing drift
Mitigations: doctor warns when pricing table is >30 days old; pricing table is a module-level dict in `src/radioactive_ralph/pricing.py`, updated on each release.

---

## 9. Out of scope (explicitly)

- Web dashboard / TUI beyond `ralph attach` streaming. `gh pr list` + terminal is sufficient.
- Multi-operator coordination. One operator per daemon per variant per repo.
- Non-git workspaces.
- Hosted / SaaS mode. Local-only.
- LLM providers other than Anthropic.
- MCP server acting as a live bridge between outer Claude session and daemon. Confirmed impossible in Claude Code 2026.
- Automatic pricing-table updates. Out-of-band release cadence.
- Variant modules published as PyPI plugins. `--variant-module` loads user-provided modules; no discovery/registry system.
