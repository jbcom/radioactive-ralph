---
title: radioactive-ralph rewrite — Go daemon, mirror workspaces, inventory-aware biases
created: 2026-04-14
status: draft
domain: product
---

# Feature: radioactive-ralph — Go rewrite with mirror workspaces and inventory-aware biases

**Created**: 2026-04-14
**Version**: 4.0 (Go pivot; supersedes v3.0 which assumed Python)
**Timeframe**: four PRs, several weeks total

## Priority: P0 (the project does not work today)

---

## Table of contents

1. Overview
2. Critical constraints
3. Architecture
4. Core concepts
   - Per-repo config directory
   - XDG state directory
   - Four workspace knobs
   - Variant-default matrix
   - Safety floors
   - Capability inventory + skill biases
   - Pre-flight wizard (capability-matching)
   - Daemon lifecycle
   - Managed-session strategy
   - Service integration (brew / launchd / systemd)
   - Skills as entry points
5. Tasks (M2 → M4, M1 already merged)
6. Acceptance criteria
7. Technical notes
8. Risks
9. Out of scope

---

## 1. Overview

radioactive-ralph M1 (merged in PR #26) deleted broken daemon claims,
stubbed non-functional Python code behind `NotImplementedError`, fixed
the marketplace install, and aligned the docs with the target
architecture. This PRD covers the remaining milestones — M2-M4 — which
are now **written in Go**, not Python.

The daemon's job is narrow and well-defined:

1. Manage `claude -p` subprocesses with `--input-format stream-json`
   stdin/stdout, resume them on death via `--resume <uuid>`.
2. Own a SQLite event log (with sqlite-vec for task dedup) that
   survives daemon crashes and multi-day runs.
3. Run a capability-matching pre-flight wizard that discovers installed
   skills/MCP servers, lets the operator pick preferences for ambiguous
   categories (multiple review skills, multiple docs-query methods),
   and persists the choices to a committed per-repo `config.toml`.
4. Apply variant profiles that encode parallelism, model tiering,
   commit cadence, termination policy, safety floors, and
   skill-preference biases.
5. Manage git worktrees off a mirror clone in XDG state, isolating all
   Ralph's work from the operator's real working tree.
6. Expose a Unix socket for `ralph status / attach / enqueue / stop`.
7. Integrate with OS service managers (`brew services` on macOS,
   `systemd --user` on Linux, WSL2+systemd on Windows) so the three
   service-appropriate variants (green, immortal, blue --daemon) can
   run as durable background services.

**Everything else — code review, PR classification, work discovery,
commits, CI checks, merge queue — happens inside the worktree Claude
sessions using whatever skills the operator has installed.** The
daemon dispatches and supervises; Claude does the work. This is the
key architectural decision that shrinks the daemon to a small, sharp
tool.

## 2. Critical constraints

### 2.1 No external injection into interactive Claude sessions

Claude Code 2.1.89+ exposes no supported mechanism to inject user-role
messages into a running *interactive* session from an external
process. The only cross-process channel is the
`--input-format stream-json` stdio protocol in headless (`-p`) mode.
Therefore the daemon never "manages" an outer Claude session — every
managed session is a `claude -p` subprocess the daemon spawned itself,
with its stdin piped.

### 2.2 The daemon does one thing well

Code review, PR merging, branch hygiene, work discovery, CI monitoring
— Claude already knows how to do all of these, and does them better
when given a good prompt and access to the right skills in the
worktree. Re-implementing any of that in the daemon violates the
single-responsibility contract and duplicates capability that already
exists inside the session.

Preserved Python modules that violated this (`reviewer.py`,
`pr_manager.py`, `work_discovery.py`, `forge/*`) are **not ported to
Go**. They stay in `reference/` for history and are deleted along with
the rest of the reference tree at v1.0.0.

### 2.3 Capability inventory shapes every spawn

What skills, MCP servers, subagents, and Claude Code plugins the
operator has installed varies per machine and per project. The daemon
discovers this inventory at launch time and uses it to *steer* each
managed session:

- If `coderabbit:review` is installed and the operator prefers it,
  `red-ralph` tells Claude "after every fix, invoke /coderabbit:review."
- If `plugin:context7:context7` is installed, `professor-ralph` tells
  Claude "during planning phase, query context7 for library docs."
- If `sec-context-depth` is installed, `blue-ralph` tells Claude
  "invoke /sec-context-depth on every diff you review."

Variant profiles declare *preferred categories* (review, security
review, docs query, debugging, brainstorming). The operator resolves
ambiguity (multiple review skills) once during `ralph init`. The
supervisor composes per-spawn system prompts based on
(variant, stage, operator preferences, actual inventory).

### 2.4 Distribution is native binaries, not pip install

Ralph is distributed as a Go binary via:

- **Homebrew tap** (`jbcom/homebrew-tap`) — primary on macOS and
  Linux, including WSL2+Linuxbrew
- **Plugin skill** — marketplace skill that bootstraps the binary on
  first run
- **`curl | sh` install script** — hosted at
  `jonbogaty.com/radioactive-ralph/install.sh`
- **Scoop + WinGet** — Windows native package managers, both published
  from one GoReleaser tag

No code signing in initial releases. All three primary install paths
bypass macOS Gatekeeper (brew formulae, `curl | sh`, and skill-invoked
shell installs do not set the `com.apple.quarantine` xattr).
Direct-download users from GitHub Releases will see a Gatekeeper
warning and can bypass with `xattr -d com.apple.quarantine <binary>`.
This is the standard FOSS practice (lazygit, zellij, mise, atuin,
hugo, asdf-vm all ship unsigned).

---

## 3. Architecture

```text
OPERATOR (~/src/myproject/)
  → runs `ralph init` once per repo (capability-matching wizard)
  → either `ralph run --variant X` (direct) or `/green-ralph` (skill)
     or `brew services start ralph-green` (for green/immortal/blue)

                 DIRECT CLI                    SKILL WRAPPER
                     │                              │
            ┌────────┴──────────┐         ┌─────────┴────────────┐
            │ pre-flight checks │         │ skill discovers,     │
            │ (tmux? gh? git?)  │         │ runs `ralph init`    │
            │ spawn supervisor  │         │ if needed, launches  │
            │ via multiplexer   │         │ `ralph run --detach` │
            └────────┬──────────┘         └─────────┬────────────┘
                     │                              │
                     └────────────┬─────────────────┘
                                  ▼
                   ┌──────────────────────────────┐
                   │  RALPH SUPERVISOR (Go)        │
                   │  backgrounded via:            │
                   │    tmux (optimal) →           │
                   │    screen (workable) →        │
                   │    syscall.Setsid fallback    │
                   │    launchd/systemd (service)  │
                   │                               │
                   │  Unix socket for IPC          │
                   │  SQLite + sqlite-vec log      │
                   │  Variant profile + inventory  │
                   └──────────────┬────────────────┘
                                  │ resolves workspace
                                  ▼
                   ┌──────────────────────────────┐
                   │   WORKSPACE MANAGER           │
                   │   isolation / object_store /  │
                   │   sync_source / lfs_mode      │
                   │                               │
                   │   shared → operator's repo    │
                   │   shallow → $XDG/shallow/     │
                   │   mirror-* → $XDG/mirror.git  │
                   └──────────────┬────────────────┘
                                  │ spawns + manages
                                  ▼
                   ┌──────────────────────────────┐
                   │  MANAGED CLAUDE SUBPROCESSES │
                   │  claude -p --input-format    │
                   │  stream-json, one per        │
                   │  worktree; daemon pipes      │
                   │  messages in, reads events   │
                   │  out, resumes on death.      │
                   │  System prompt injects       │
                   │  variant's skill biases.     │
                   └──────────────────────────────┘
                                  ▲
                                  │  ralph attach / ralph status
                                  │  via Unix socket
                                  │
                          OPERATOR (other terminal)
```

XDG state layout:

```text
$XDG_STATE_HOME/radioactive-ralph/        (on macOS: ~/Library/Application Support/radioactive-ralph/)
└── <repo-hash>/                          # sha256(abspath(operator-repo))[:16]
    ├── mirror.git/                       # only if any variant uses mirror-* isolation
    ├── shallow/                          # only if any variant uses shallow isolation
    ├── worktrees/
    │   └── <variant>-<n>/
    ├── inventory.json                    # capability discovery output
    ├── state.db                          # SQLite + sqlite-vec event log
    ├── state.db-wal
    └── sessions/
        ├── <variant>.sock                # per-variant Unix socket
        ├── <variant>.pid                 # supervisor PID + flock
        ├── <variant>.alive               # heartbeat mtime
        └── <variant>.log                 # supervisor stdout/stderr
```

Operator's repo stays clean — only `.radioactive-ralph/config.toml`
and `.radioactive-ralph/.gitignore` are committed;
`.radioactive-ralph/local.toml` is gitignored for operator-specific
overrides.

---

## 4. Core concepts

### 4.1 Per-repo config directory

```text
~/src/myproject/.radioactive-ralph/
├── config.toml          # committed: capabilities, variant policy, workspace defaults
├── .gitignore           # committed: excludes local.toml
└── local.toml           # gitignored: operator-specific (tmux pref, log level, etc.)
```

`ralph init` creates this tree, runs the capability-matching wizard,
writes both TOML files, and appends `.radioactive-ralph/local.toml` to
the repo's root `.gitignore`. Missing `config.toml` = refuse to run
with a clear message.

`config.toml` example post-init:

```toml
# Auto-generated by `ralph init`. Re-run `ralph init --refresh` to
# detect newly-installed skills without losing your choices.

[capabilities]
# Skills Ralph biases toward during certain tasks. Operator chose
# these during init from the set of actually-installed skills.
review          = "coderabbit:review"
security_review = "sec-context-depth"
docs_query      = "plugin:context7:context7"
brainstorm      = "superpowers:brainstorming"
debugging       = "superpowers:systematic-debugging"

# Skills the operator doesn't want Ralph to bias toward even if present.
disabled_biases = []

[daemon]
default_object_store       = "reference"
default_lfs_mode           = "on-demand"
copy_hooks                 = true
allow_concurrent_variants  = true

[variants.green]
# Empty section = use the variant's hardcoded defaults.

[variants.red]
review_bias = "coderabbit:review"

[variants.blue]
security_review_bias = "sec-context-depth"

[variants.immortal]
object_store = "full"   # multi-day discipline

[variants.old_man]
# Safety floor: object_store = "full" is pinned. See docs.
```

### 4.2 XDG state directory

`$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/` via Go's
`platformdirs`-equivalent (stdlib `os.UserConfigDir` + fallback to
`$XDG_STATE_HOME`). Per-repo per-machine.

`<repo-hash>` = `sha256(abspath(operator-repo-root))[:16]`, stable per
location. Operator cloning the same repo to two paths gets two
independent workspaces.

### 4.3 Four orthogonal workspace knobs

| Knob | Values | Default | Purpose |
|------|--------|---------|---------|
| `isolation` | `shared`, `shallow`, `mirror-single`, `mirror-pool` | varies by variant | Where the work happens |
| `object_store` | `reference`, `full` | `reference` unless floor-pinned | Share operator's objects or fully clone |
| `sync_source` | `local`, `origin`, `both` | `both` | Where the mirror fetches from |
| `lfs_mode` | `full`, `on-demand`, `pointers-only`, `excluded` | `on-demand` | How LFS content is handled |

Safety floors can pin any knob for risky variants. Precedence: CLI
flags > env vars (`RALPH_*`) > `config.toml` per-variant > `config.toml`
`[daemon]` default > variant profile default > project default.

### 4.4 Variant-default matrix

| Variant | Isolation | Parallel | Object store | LFS | Gate |
|---------|-----------|----------|--------------|-----|------|
| blue | `shared` (default, operator choice) | 0 | n/a | pointers-only | — |
| grey | mirror-single | 1 | reference | pointers-only | — |
| red | mirror-pool | 8 | reference | on-demand | — |
| green | mirror-pool | 6 | reference | on-demand | — |
| professor | mirror-pool | 4 | reference | on-demand | — |
| fixit | mirror-single | 1 | reference | pointers-only | — |
| immortal | mirror-pool | 3 | full | pointers-only | — |
| savage | mirror-pool | 10 | reference | on-demand | `--confirm-burn-budget` |
| old-man | mirror-single | 1 | **full (floor)** | on-demand | `--confirm-no-mercy` |
| world-breaker | mirror-pool | 10 | **full (floor)** | full | `--confirm-burn-everything` |

### 4.5 Safety floors

| Variant | Floor | Override path |
|---------|-------|---------------|
| Any with `shared` isolation | Tool allowlist must exclude `Edit` + `Write` | Impossible (compile-time validator) |
| old-man | `object_store = full`; refuses default branch; fresh gate per invocation | Two CLI flags (both `--confirm-no-mercy` and `--confirm-shared-objects-unsafe`) + config override |
| world-breaker | `object_store = full`; spend cap required; fresh gate per invocation | Two-step override + explicit spend cap |
| savage | Spend cap required; fresh gate per invocation | Operator raises cap with `--spend-cap-usd N` |
| savage, old-man, world-breaker | Refuse to run when invoked under launchd/systemd (service context) | No override — service-unsafe by design |

Default-branch detection uses `git symbolic-ref refs/remotes/origin/HEAD`,
falling back to the hardcoded list
`{main, master, trunk, develop, production, release/*}`.

### 4.6 Capability inventory and skill biases

Discovered at `ralph init` (for operator review) and at each `ralph run`
(for runtime steering). Shell-based discovery:

- Enumerate `~/.claude/skills/*/SKILL.md` frontmatter `name` field
- Parse `~/.claude/settings.json` for `mcpServers`, `enabledPlugins`,
  `extraKnownMarketplaces`
- Parse `.claude/settings.json` in the repo for project-scoped additions
- Enumerate `~/.claude/plugins/cache/*/` directories
- Call `claude plugin marketplace list --json` if the CLI exposes it

Stored as `inventory.json`:

```json
{
  "generated_at": "2026-04-14T20:45:00Z",
  "claude_version": "2.1.89",
  "skills": ["coderabbit:review", "context7:query-docs", "..."],
  "mcp_servers": [{"name": "context7", "reachable": true}],
  "agents": ["code-reviewer", "feature-dev:code-explorer"],
  "environment": {"gh_authenticated": true, "in_claude_code": true}
}
```

Variant profiles declare `SkillBiases map[BiasCategory]BiasSnippet`.
At spawn time, supervisor:

1. Reads config.toml `[capabilities]` + `[variants.<name>]` overrides.
2. Reads inventory.json.
3. For each bias category the variant cares about, picks the operator's
   preferred skill if available in inventory, else falls back to
   variant's default, else skips silently.
4. Injects the chosen bias snippets into the system prompt via
   `--append-system-prompt`.

Missing biases are logged at INFO. `ralph doctor` flags drift.

### 4.7 Pre-flight wizard (capability-matching)

`ralph init` is a first-class CLI command, not just a config bootstrap.

1. **Discover**: run shell commands to enumerate skills, MCP servers,
   agents, plugins.
2. **Categorize**: group findings by capability type (review,
   security review, docs query, etc.). Multi-candidate categories are
   flagged for operator input.
3. **Ask**: for each multi-candidate category, prompt operator to pick
   preferred or explicitly disable. Single-candidate categories
   auto-select with a one-line confirmation.
4. **Suggest per-variant defaults**: "red-ralph will bias toward
   coderabbit:review during fix cycles — override?"
5. **Write**: config.toml + local.toml + `.gitignore` entries. All
   choices commented with alternatives for later edit.

`ralph init --refresh` re-discovers and proposes updates without
losing existing choices.

Universal pre-flight checks run at `ralph run` start (not `init`):

- `gh auth status` green
- `claude --version` within pinned supported range
- Working tree clean (offer to stash/branch if not)
- Not on default branch (offer to branch if so, for destructive variants)
- Multiplexer available (tmux → screen → setsid)

`--yes` skips non-blocking checks; blocking checks still require answer
(operator can pre-answer in config.toml).

### 4.8 Daemon lifecycle

1. **Entry**: `ralph run --variant X` resolves profile + overrides,
   runs pre-flight, if OK spawns supervisor via multiplexer (or runs
   in-foreground for `--foreground` / service invocation).
2. **Supervisor boot**:
   - Acquire flock on `<variant>.pid` (refuses if live supervisor)
   - Open SQLite in WAL + load sqlite-vec extension (soft-fail to FTS5)
   - Bind Unix socket
   - Start heartbeat goroutine (touches `<variant>.alive` every 10s)
   - Load variant profile + resolved workspace config + inventory
   - Replay event log to rebuild in-memory state
   - Protocol-ping: spawn throwaway `claude -p`, send "reply PONG",
     verify parse. Abort boot on failure.
   - Initialize WorkspaceManager per isolation mode
   - Start session pool
3. **Workspace init (first run for variant)**: WorkspaceManager creates
   mirror/shallow/worktree as needed. Copies operator's `.git/hooks/`
   to mirror's `.git/hooks/` unless `copy_hooks = false`. Detects LFS
   usage. Applies knobs.
4. **Session pool**: for each slot, create/reuse worktree, spawn
   `claude -p --input-format stream-json --session-id <uuid>` with
   stage-appropriate system prompt (variant + inventory biases).
   Record session in SQLite.
5. **Event loop**: read stream-json events from each session, append
   to event log (parsed + raw for protocol-drift resilience), act on
   result events. On subprocess exit, classify:
   - clean → task done, pick next
   - context-exhausted → resume with `--resume <uuid>` + sentinel
     re-prompt ("what task ID are you working on?")
   - rate-limited → exponential backoff, resume when clear
   - crashed → log, resume once, escalate to operator on repeat
   - operator-killed → graceful drain
6. **IPC**: Unix socket serves `status`, `attach`, `enqueue`, `stop`,
   `reload-config` as JSON lines.
7. **Termination**: per variant policy. Drain events, close socket,
   remove PID, clean worktrees per variant rule, exit.

### 4.9 Managed-session strategy

Every managed invocation:

```bash
claude -p --bare \
  --input-format stream-json \
  --output-format stream-json \
  --include-partial-messages --verbose \
  --permission-mode <acceptEdits | bypassPermissions> \
  --allowedTools <variant.ToolAllowlist> \
  --model <variant.ModelForStage(currentStage)> \
  --session-id <stableUUID> \
  --append-system-prompt <variant-system-prompt-with-inventory-biases>
```

- `--bare` skips project CLAUDE.md / hook / MCP auto-discovery for
  reproducibility. Variant provides system prompt via
  `--append-system-prompt`.
- `--permission-mode default` never used (no TTY for interactive prompts).
- `acceptEdits` for normal variants; `bypassPermissions` for old-man /
  world-breaker behind their gates.

Spend tracking: supervisor parses `usage` field from stream-json result
events, accumulates per-model in SQLite, computes USD from pricing
table. Cap-hit → graceful stop, voiced as
*"Ralph burned his allowance. Going home."*

Session ID pinning via `--session-id <uuid>` avoids Claude Code issue
#44607 (interactive mode ID ≠ on-disk filename; does not affect `-p` mode).

### 4.10 Service integration

Three variants are eligible to run as persistent system services:

| Variant | Service suitable? | Scheduler kind |
|---------|-------------------|----------------|
| green | ✅ always-on daemon | `KeepAlive=true` |
| immortal | ✅ always-on, survives reboot | `KeepAlive=true` + `RunAtLoad=true` |
| blue (with `--daemon`) | ✅ PR observer | `KeepAlive=true` |
| grey | ⏰ timer, not daemon | weekly `StartCalendarInterval` / systemd `.timer` |
| professor | ⏰ timer, not daemon | daily `.timer` |
| red, fixit | ❌ on-demand only | (value in operator reading the output) |
| savage, old-man, world-breaker | ❌ refuse service context | fresh confirmation gate required |

The CLI exposes `ralph service install --variant X` that emits the
appropriate plist (macOS), systemd user unit (Linux), or Scoop
persistence entry (Windows), and wraps `brew services` / `systemctl
--user` commands.

`--foreground` flag on `ralph run` runs the supervisor inline (stdout /
stderr to caller), bypassing the multiplexer. This is what the service
unit's `ExecStart` invokes so launchd / systemd is the actual
supervisor.

Floor-gated variants detect service context (env vars
`LAUNCHED_BY=launchd` or `INVOCATION_ID=*` for systemd) and refuse to
run, printing a clear error and exiting non-zero. Detected at pre-flight,
before any workspace setup.

### 4.11 Skills as entry points

Each `skills/<variant>/SKILL.md` ≤30 lines. Skeleton:

```markdown
---
name: green-ralph
description: The Classic. Unlimited loop, sensible model tiering. Launches the ralph daemon in the background.
---

If `.radioactive-ralph/config.toml` is missing, run `ralph init`
interactively and wait for the operator to review.

Otherwise shell out: `ralph run --variant green --detach`

The daemon's own pre-flight checks, Ralphspeak banter, and handoff
message handle everything else. Return whatever the CLI prints to the
operator.
```

Rich behavioral lore moves into `internal/variant/green.go` docstrings
and `docs/variants/green-ralph.md` which auto-regenerates from
`Profile` at build time (M3).

### 4.6 Plans discipline + Fixit as advisor

Every repo using radioactive-ralph must have `.radioactive-ralph/plans/index.md`
with YAML frontmatter (`status`, `updated`, `domain`,
`variant_recommendation`) referencing at least one real plan file.
Alternatively, `[daemon] plans_dir = "xdg"` in `config.toml` puts plans
under XDG state instead of in-repo (private plans on public repos).

Nine of the ten variants **refuse to run** without a valid plans
setup. The refusal message directs operators to `ralph run --variant
fixit` — because `fixit-ralph` is the **only** variant capable of
reasoning about which Ralph to use.

Fixit auto-detects its mode from the plans state:

- **Advisor mode** — no plans dir, malformed `index.md`, or
  `--advise` passed. Walks the codebase + any provided description,
  writes a recommendation to
  `.radioactive-ralph/plans/<topic>-advisor.md`. Primary variant
  always; alternate only when real tradeoffs exist. If Fixit
  recommends itself that's fine — still the only Ralph that made the
  call. With `--auto-handoff` AND high confidence AND no tradeoffs,
  Fixit spawns the recommended variant as a follow-up.

- **ROI banger mode** — valid plans setup present. Classic Joe-Fixit
  persona: N cycles, highest-ROI task per cycle, ≤5 files / ≤200 LOC
  PRs, bill at the end.

This structure enforces plans-first discipline: no variant grinds
against a repo that hasn't had its scope examined, and exactly one
variant knows how to do that examination. The rename from
`joe-fixit-ralph` to `fixit-ralph` (2026-04-14) reflects this
expanded role — the Joe-Fixit persona is preserved as character
flavor, but the variant does more than ROI now.

---

## 5. Tasks

### M1 (merged, PR #26)

Done. Marketplace fix, README/docs truthfulness, dead Python code
removed, broken implementations stubbed behind `NotImplementedError`,
two broken tests fixed, all canonical domain docs rewritten, PRD
committed.

### M2 — Go rewrite of the daemon (this branch)

Subdivided into commits so the git log tells the story. Each commit
passes `go test ./...` + `golangci-lint run`.

- **M2.T0 — Python → reference/** ✅ (first commit on this branch)
- **M2.T1 — Go bootstrap**: `go.mod`, `cmd/ralph/main.go` with kong
  CLI skeleton, `Makefile`, GitHub Actions CI (go test, golangci-lint,
  govulncheck), replace existing Python workflows.
- **M2.T2 — `internal/xdg` + `internal/config`**: repo-hash derivation,
  state-dir path helpers, kong CLI struct, TOML loader, precedence
  `Resolve()` function, safety floor enforcement.
- **M2.T3 — `internal/inventory`**: shell discovery of skills / MCPs /
  plugins, JSON roundtrip, consumed by init + supervisor.
- **M2.T4 — `internal/db`**: SQLite + sqlite-vec (via `modernc.org/sqlite`
  + `asg017/sqlite-vec-go-bindings`), WAL, schema migrations,
  `EventLog.Append` / `.Replay`, tasks + sessions + task_vec tables,
  raw + parsed payload storage.
- **M2.T5 — `internal/ipc`**: net.UnixListener server + client,
  JSON-line protocol, `status` / `attach` / `enqueue` / `stop` /
  `reload-config` commands, heartbeat file mtime for liveness.
- **M2.T6 — `internal/multiplexer`**: tmux / screen / syscall.Setsid
  probe, `SpawnDetached(cmd, logPath, pidPath)`.
- **M2.T7 — `internal/workspace`**: IsolationMode / ObjectStoreMode /
  SyncSourceMode / LfsMode enums, `WorkspaceManager` dispatching on
  isolation, mirror init with `--reference`, two-remote fetch
  (`local` / `origin`), worktree add/remove/reconcile, LFS detection
  + per-mode config, repack-on-corruption recovery.
- **M2.T8 — `internal/session`**: `ClaudeSession` wrapping `claude -p`
  via `exec.CommandContext`, stream-json stdin/stdout (bufio Scanner),
  `SendUserMessage` / `Events` / `Interrupt` / `WaitForIdle` / `Resume`,
  protocol-ping handshake, sentinel re-prompt after resume with task-ID
  verification, `PromptRenderer` combining variant + inventory biases.
- **M2.T9 — `internal/service`**: `ralph service install/uninstall/list/status`
  commands, platform-dispatching among launchd / systemd-user /
  brew-services, service-context detection in pre-flight.
- **M2.T10 — `internal/supervisor`**: PID flock, socket bind, event
  replay, session pool, IPC request loop, multi-variant coexistence via
  per-variant paths, graceful termination.
- **M2.T11 — `internal/initcmd`**: capability-matching wizard, writes
  `config.toml` + `local.toml`, appends to repo `.gitignore`, supports
  `--refresh` to re-discover without losing choices.
- **M2.T12 — `internal/doctor`**: environment health — git version,
  claude version range, gh auth, tmux/screen/setsid, platformdirs
  writable, config.toml present.
- **M2.T13 — `internal/voice`**: Ralph personality templates (thin in
  M2, variant-specific fills in M3).
- **M2.T14 — `cmd/ralph/main.go`**: kong CLI wiring —
  `init / run / status / attach / stop / doctor / service / version` +
  hidden `_supervisor`.
- **M2.T15 — Homebrew tap + GoReleaser + install script**: create
  `jbcom/homebrew-tap` repo via `gh`, `.goreleaser.yaml` with brew +
  Scoop + WinGet + tarballs + checksums + cosign provenance, install
  script at `docs/install.sh`, docs page explaining the three install
  paths.
- **M2.T16 — Integration tests**: always-on `ralph init` + `ralph run
  --variant blue --detach` round-trip. Gated-on-CLAUDE_AUTHENTICATED
  real `claude -p` spawn + resume + sentinel verification.
- **M2.T17 — macOS + Linux CI matrix**: run integration tests on both
  platforms. Windows is deferred (WSL2+systemd coverage is enough).

### M3 — Ten variants + pre-flight + voice + worktree orchestration

- **M3.T1 — `internal/variant/profile.go`**: `Profile`,
  `Stage`, `Model`, `TerminationPolicy`, `PreflightQuestion`,
  `VoiceProfile`, `SafetyFloors`, `BiasCategory`, `BiasSnippet` types.
- **M3.T2 — Ten variant files**: `internal/variant/{green,grey,red,
  blue,professor,joe_fixit,immortal,savage,old_man,world_breaker}.go`.
  Each ≤300 LOC, each faithful to the current SKILL.md.
- **M3.T3 — Safety floors enforcement**: compile-time validators for
  `shared`-isolation tool allowlist; runtime two-step override for
  destructive variants' object_store.
- **M3.T4 — Dry-run + spend-cap**: first-invocation dry-run mode for
  risky variants; spend-cap tracking parses stream-json `usage` events.
- **M3.T5 — `internal/voice` fills**: per-variant voice template library,
  used by pre-flight questions, status output, attach stream,
  handoff messages.
- **M3.T6 — Pre-flight wizard as question registry**: severity-tiered
  (blocking / warning / info), CLI + skill renderers share the registry.
- **M3.T7 — Skills rewritten to ≤30 lines**: each SKILL.md delegates
  to `ralph run`.
- **M3.T8 — Auto-generated variants-matrix.md**: `cmd/matrixgen` reads
  every `Profile`, emits `docs/reference/variants-matrix.md`.
  CI drift check fails on diff.
- **M3.T9 — Per-variant docs pages**: `docs/variants/*.md` auto-generated
  from profiles + hand-written lore sections allowed above/below.
- **M3.T10 — Unit tests per variant**: parameterized test asserts
  every profile's fields in range, tool allowlist subset of known
  tools, gated variants declare gates, blue excludes Edit+Write, grey
  is single-pass with max_parallel=1, safety floors non-overridable.

### M4 — Integration harness + doctor + service install + release 1.0.0

- **M4.T1 — Integration scenarios**: full grey-ralph single-pass
  end-to-end against fake origin; green-ralph SIGTERM mid-run;
  session death + resume continuity; pre-flight refusal for old-man
  on default branch; safety-floor override two-step; mirror `--reference`
  + repack-on-corruption recovery; LFS detection applied per-variant;
  hook preservation (operator's pre-commit runs inside mirror); multi-variant
  coexistence (green + grey simultaneous supervisors).
- **M4.T2 — Service install integration**: `ralph service install
  --variant green` writes plist/unit correctly; `brew services start`
  invokes supervisor in foreground; service-context detection refuses
  floor-gated variants.
- **M4.T3 — `ralph doctor` comprehensive**: exit 0 green, exit 1 with
  ranked remediation. Checks git version, claude version range, gh
  auth, tmux/screen/setsid fallback, platformdirs writable,
  sqlite-vec loadable, workspace config.toml present, inventory fresh.
- **M4.T4 — Docs completion**: final architecture diagram, UX
  walkthrough with both CLI + skill + service paths, auto-generated
  variants matrix committed.
- **M4.T5 — Release 1.0.0**: GoReleaser publishes to GitHub Releases
  + Homebrew tap + Scoop bucket + WinGet (microsoft/winget-pkgs PR).
  Install script live at `jonbogaty.com/radioactive-ralph/install.sh`.
  `assets/demo.gif` records full `ralph init` → `ralph run --variant grey`
  → PR opens → `ralph stop`.
- **M4.T6 — Purge `reference/`**: delete the entire Python tree in one
  commit at release. `radioactive-ralph==0.5.1` on PyPI remains
  available; no further Python releases.

## 6. Acceptance criteria

### M2
- `go test ./...` passes locally and in CI on both macOS and Linux
- `golangci-lint run` clean
- `govulncheck ./...` clean
- `ralph --version` prints the compiled version
- `ralph init` in a fresh temp repo creates `.radioactive-ralph/`
  correctly and writes a sensible starter `config.toml` with
  capability discoveries
- `ralph run --variant blue --detach` launches a backgrounded
  supervisor; `ralph status` returns healthy; `ralph stop` terminates
  cleanly; no leftover PID/socket/worktree
- `ralph doctor` exit 0 on healthy machine, exit 1 with remediation
  list on broken
- Gated integration test (real `claude -p`, `CLAUDE_AUTHENTICATED=1`):
  spawn, inject message, verify response event, kill, resume, sentinel
  re-prompt succeeds
- `.claude-plugin/marketplace.json` validates under
  `claude plugin validate .` (unchanged from M1)
- GoReleaser dry-run succeeds: `goreleaser release --snapshot --clean`
  produces all artifacts (brew formula, Scoop manifest, WinGet manifest,
  tarballs, checksums)
- Homebrew tap repo `jbcom/homebrew-tap` exists with an initial
  Formula/ralph.rb placeholder

### M3
- All 10 variant profiles type-check and pass `golangci-lint`
- Safety-floor tests pass (single-flag rejected, two-step accepted)
- `grey`: single-pass with `MaxParallel == 1`
- `blue`: excludes Edit + Write from tool allowlist (compile-time)
- Auto-generated variants-matrix.md is stable across consecutive runs
- Skills ≤30 lines each

### M4
- All integration scenarios pass (gated ones skip cleanly without
  `CLAUDE_AUTHENTICATED`)
- `ralph doctor` passes on both macOS and Linux CI runners
- `ralph service install --variant green` installs and can be
  `brew services start`-ed on macOS CI
- 1.0.0 published to GitHub Releases, Homebrew tap, Scoop, WinGet
- Install script at `jonbogaty.com/radioactive-ralph/install.sh` works
  end-to-end on macOS and Linux
- `reference/` deleted

## 7. Technical notes

**Go deps (target)**:
- `github.com/alecthomas/kong` — CLI parsing
- `github.com/BurntSushi/toml` or `github.com/pelletier/go-toml/v2` —
  config TOML
- `modernc.org/sqlite` — pure-Go SQLite (no CGo)
- `github.com/asg017/sqlite-vec-go-bindings` — vec0 virtual table
- `github.com/gofrs/flock` — cross-platform file locking
- `github.com/google/uuid` — session IDs
- stdlib: `os/exec`, `encoding/json`, `bufio`, `net`, `syscall`,
  `context`, `sync`, `log/slog`

**No Anthropic SDK**. The daemon speaks to Claude only through the
`claude -p` binary. If we ever need a direct Anthropic API call (we
don't in the current design), it's a ~100-line `net/http` struct.

**No GitHub API client**. Forge interactions happen inside worktree
Claude sessions via the `gh` CLI, which Claude already knows how to
drive.

**`--bare` flag on every managed spawn** for reproducibility.
Supervisor owns the system prompt via `--append-system-prompt`.

**Session ID strategy**: stable UUID per session, pinned via
`--session-id <uuid>`. Supervisor stores in SQLite. Resume reuses.

**sqlite-vec vs FTS5**: prefer sqlite-vec for semantic task dedup;
soft-fail to FTS5 if extension loading fails at runtime.

**State directory**: `$XDG_STATE_HOME/radioactive-ralph/<repo-hash>/`
on Linux / WSL; `~/Library/Application Support/radioactive-ralph/
<repo-hash>/` on macOS per Apple conventions.

**Cross-platform daemon detach**: `syscall.Setsid()` + `fork/exec`
pattern for the Python-free fallback (Linux + macOS). Windows is not
supported for the `ralph run` direct invocation; Windows users invoke
via WSL2+Linuxbrew where the Linux path works. The `ralph` binary on
Windows only supports `ralph init / status / doctor` (config-manipulation
commands); the supervisor requires a POSIX environment.

**Release tooling**: GoReleaser generates brew, Scoop, WinGet artifacts
from one `.goreleaser.yaml` on git tag push. No code signing in initial
releases. cosign for supply-chain provenance.

## 8. Risks

### R1 — Stream-json protocol drift
Pin `claude` version range in doctor. Protocol-ping on supervisor boot.
Raw + parsed payload storage so forward-compat fixes can replay old
events.

### R2 — Session file format drift
Sentinel re-prompt with task-ID verification after every resume.
Task-state checkpoints rich enough to reseed a fresh session if resume
fails.

### R3 — Pre-flight UX complexity
Silent-when-passing detectors. Remembered answers in config.toml.
`--yes` flag. Three severity levels.

### R4 — Multiplexer quirks on macOS
tmux strongly recommended (doctor top-priority remediation). Stdlib
`syscall.Setsid` fallback instead of external daemon library.
Heartbeat file detects liveness independently of PID.

### R5 — Risky variants running unattended
Per-variant safety floors (`object_store = full` pinned for destructive
ones; two-step override). Spend caps. First-invocation dry-run consent.
Mirror-based isolation keeps destructive ops in XDG, never operator's
working tree. Service-context detection refuses floor-gated variants
under launchd/systemd.

### R6 — Scope creep
Explicit out-of-scope list. Public API = CLI + `Profile` fields.
No internal package is semver-stable.

### R7 — `config.toml` teammate breakage
Two-file split (`config.toml` committed, `local.toml` gitignored per-
operator). `ralph init --local-only` bootstraps a teammate's local.toml.

### R8 — Multi-variant race conditions
Per-variant-scoped paths (`<variant>.sock`, etc.). SQLite WAL handles
interleaved writes. `allow_concurrent_variants` config toggle.

### R9 — Shared-object reference corruption
Repack-on-corruption recovery. Destructive variants default to `full`
object store. Integration test simulates aggressive `git gc`.

### R10 — LFS surprises
Auto-detect on init. Variant-appropriate defaults. `excluded` mode
refuses tasks that touch LFS paths with clear error.

### R11 — Hook skipping
Copy operator's `.git/hooks/` into mirror on init. `copy_hooks = false`
opt-out.

### R12 — Spend-tracking pricing drift
Pricing table is a generated constant updated on each release. Doctor
warns when >30 days old.

### R13 — GoReleaser cascade failures
GitHub Actions release.yml pins GoReleaser version. Brew tap / Scoop
bucket / WinGet PR each retriable.

### R14 — Inventory staleness
`ralph init --refresh` re-discovers without losing choices. Supervisor
logs at INFO when config.toml references skills not in inventory.

## 9. Out of scope

- Web dashboard beyond `ralph attach` streaming
- Multi-operator coordination (one operator per daemon per variant per repo)
- Non-git workspaces
- Hosted / SaaS mode
- LLM providers other than Anthropic
- MCP server acting as a live bridge (confirmed impossible in Claude Code 2026)
- Automatic pricing-table updates (out-of-band release cadence)
- Variant modules published as separate packages (operator customization via config.toml is the extension story)
- Direct internal git operations in the daemon (every git op happens via Claude in a worktree, or via `os/exec` in the workspace manager for mirror/worktree management only)
- Direct forge API calls in the daemon (Claude uses `gh` CLI in worktrees)
- Direct Anthropic API calls in the daemon (Claude runs via `claude -p` subprocesses)
- Windows `ralph run` direct invocation (WSL2+Linuxbrew is the Windows path)
- macOS code signing / notarization (unsigned FOSS binaries are the ecosystem standard; brew + curl|sh bypass Gatekeeper)
