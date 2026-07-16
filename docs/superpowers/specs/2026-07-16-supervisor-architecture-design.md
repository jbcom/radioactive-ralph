# radioactive-ralph — Supervisor Architecture (design)

Status: approved-in-brainstorm, 2026-07-16. This is the comprehensive
architecture that supersedes the current durable-service / per-repo-plandag /
committed-`.radioactive-ralph/`-dir model. It is implemented on one large
branch (`feat/supervisor-architecture`) with one comprehensive directive set,
because the coordination protocol, database location, and config model are
interdependent and cannot be split without designing the seams twice.

Rationale for every decision below is in `.agent-state/decisions.ndjson`.

## 1. The control invariant (non-negotiable)

An underlying agent CLI must **never** block the system — no permission
prompts, no clarification waits, no interactive menus. Ralph is the supervisor
at all times. Nobody *interacts* with the agents; the human gets a **read-only**
view. Every agent runs **non-interactively**, under Ralph's **direct pty
ownership**, and Ralph continuously watches each stream for progress vs.
stall/prompt/no-output and acts (auto-resolve, deny, or kill-and-reclaim) —
never waits.

## 2. Substrate: `creack/pty`, not tmux

Ralph owns each agent's pty directly via `creack/pty` (MIT, the de-facto
standard Go pty library, real Windows ConPTY support). Per agent Ralph holds
`*os.File` (the ptmx) + `cmd.Process`:

- **Input**: direct `Write()` to the ptmx (or, in practice, agents run
  non-interactively and need little/no input).
- **Kill**: `Process.Kill()` — instant, and cheap because state is durable
  (§6): recovery = replay the agent's plan-scoped context to a fresh process.
- **Output**: tee the ptmx reader — one branch feeds the live pane buffer, one
  feeds the watchdog. No fifo/`pipe-pane` indirection; Ralph owns the fd.

tmux was evaluated (via `gotmux`) and **rejected**: the tmux server owns the
pty, so every read/write/kill becomes an `os/exec` round-trip to a process
Ralph doesn't own — it fails the control invariant, adds an external-binary
failure domain, and has an unverified native-Windows story (`psmux`). 3mux and
sunder were also rejected (see decisions log). The full 4-way code-study
evaluation is preserved and will be committed alongside this branch's
implementation notes.

## 3. Hybrid agent I/O

The **pane** is for human observation only. **Structured data** (result JSON,
usage/cost for spend caps) is read from a **file/fd Ralph passes to the CLI**
(`claude --print`, `opencode run --format json`, or a redirect for CLIs that
can't stream to a file) — **never scraped** from the rendered terminal, which is
lossy. Agent output is **flushed to rotating, compressed logs** (structured
logging), so memory stays flat over days rather than accumulating scrollback.

## 4. One binary, two modes: supervisor + dumb client

A single `radioactive_ralph` binary:

- **`radioactive_ralph --supervisor`** — the process that holds everything
  open: all agent ptys **and** headless project work. Distributed via the Go
  service framework (launchd/systemd/Windows service) **or** runnable under
  nohup/screen/anything. It is the control layer and the durable authority.
  Refuses to start if it detects another supervisor already listening.
- **`radioactive_ralph`** (no flag) — a **dumb client**. It exists only to
  (a) talk to the supervisor and (b) initialize config. It **refuses to run**
  unless a supervisor is listening (offering to start one). It renders the
  read-only TUI (§7). It owns no ptys, no DB, no business logic.

Both modes share the **same initialization logic** — a wizard TUI, or flags, or
an existing config passed by path — the client for project config, the
supervisor for user-level config.

## 5. Supervisor discovery (the socket is the advertisement)

The supervisor binds a socket at a well-known **XDG runtime path**: a Unix
domain socket on macOS/Linux, a named pipe on Windows (the existing
`internal/ipc` dual-transport). Discovery and single-instance both fall out of
this, reusing machinery the repo already has:

- **Client discovery**: try to connect. Success → supervisor live and
  reachable. Failure → no supervisor → refuse / offer to start one.
- **Single-instance**: binding the socket *is* the mutex (a second `Listen`
  fails). A **PID lockfile** (`flock` + PID — existing
  `internal/runtime/flock.go`) plus the heartbeat file distinguish a *live*
  supervisor from a *stale* socket left by a crashed one (dead PID → reclaim:
  remove the stale socket, take over).

## 6. Storage: ONE user-level SQLite DB; clean repos

There are **no per-project SQLite databases** and **no committed
`.radioactive-ralph/` directory**. Instead:

- **One user-level SQLite database** (XDG data dir) is the durable memory for
  **all** projects: the plan DAG state, per-project **config**, process
  tracking, spend accounting, and session/role history. Because the supervisor
  always runs first and always, it always knows "what's next" in every
  project's plan.
- The supervisor takes **regular backups** of this DB.
- **Repos stay clean** — zero committed directories by default. A user *may*
  opt in by pointing config/DB at an in-repo path, or git-track their user
  config (documented), but it is never the default. This eliminates the
  collision/merge-conflict/repo-litter problems of file-based per-repo state.
- This is explicitly **not** the current `.agent-state`-skill pattern.

A "project" is identified by its repo path (a key in the user DB), not by a
marker directory. The XDG user-level config recommends **resource thresholds**
(memory/CPU) derived from the host's configuration.

## 7. TUI: read-only, macro → meso → micro

The client TUI (Bubble Tea + Lipgloss) is a **read-only, seamless live view** —
"attach/detach" is the wrong nomenclature; running the client simply shows the
supervisor's live state. Three drill-down levels:

- **Macro**: the project plan + overall hierarchy.
- **Meso**: drill into the plan to converse with the PM / team-lead role about
  it; drill the hierarchy to squads (if any) or a singular worker.
- **Micro**: one worker — its live pane / log tail.

## 8. Resource + liveness watchdog

Per agent process the supervisor watches: **no-output-for-N** (stall),
**PID/process health**, **memory/CPU** vs. configurable thresholds (safe
defaults recommended from system config), and **stream-content** signals
(permission prompts, clarification requests). Any would-be block → auto-resolve
/ deny / kill-and-reclaim. This *is* the never-block invariant, enforced by
Ralph's own logic (the substrate only provides the fd).

## 9. Providers (local-only)

Shipped providers: **`claude`, `codex`, `opencode`** (+ `agy`/Antigravity
pending a spike). "Local-only" = **no cloud control surface** in the loop;
calling a hosted model API for inference is fine. `gemini` removed (deprecated
2026-06-18, backend 410 Gone). `cursor-agent` excluded (delegates the session
to Cursor's cloud). `opencode` bound via its local `run` path only.

## 10. Roles / squads / the coordination layer (designed here, this branch)

The conflated "variant" is reconsidered as part of this work. The layering is
hierarchical (industry-standard **A2A orchestrator↔worker**, not the invented
"mayor protocol"): the supervisor dispatches from the plan DAG to a **PM /
team-lead** role, which coordinates **worker** roles (archetypes such as
technical-writer, frontend-dev) that Ralph can **clone and mutate into squads**
with path/worktree-based isolation. The **plan DB is the scoped context** each
worker reads — a durable, execution-scoped slice, not a giant context dump
(the A2A insight: a task is id + lifecycle + updatable state, which the plan
model already matches). **Fixit ↔ Professor** form the durable, self-correcting
planning loop (Fixit decomposes/recommends → Professor executes with reflection
→ reflection re-plans). Cost transparency and progress propagate **up** this
chain to the macro TUI.

**Open, resolved during implementation on this branch (not before):** the exact
role/squad primitive — whether "variant" is replaced by a role×profile split,
and the concrete squad clone/mutate mechanics — is deliberately deferred to the
first-principles **variant-lineup audit** (a directive work-unit), because the
audit determines the lineup that this layer is built on. The layering *shape*
above (supervisor → PM/team-lead → workers, plan-DB-as-scoped-context, Fixit↔
Professor loop) is settled; the primitive it's expressed in is the audit's
output.

## 11. What survives, what changes

- **Survives / is vindicated**: `internal/ipc` (repurposed: attach → discover),
  `internal/runtime/flock.go` (PID lock), `internal/service` (supervisor
  distribution), `internal/xdg`, `internal/provider` (minus gemini), the plan
  DAG *model* (moves into the single user DB), Bubble Tea TUI.
- **Changes / is removed**: per-repo `plans.db` and the committed
  `.radioactive-ralph/` config dir → one user-level DB; the open-ended
  "configure a CLI however" provider model → constrained supervised execution;
  the "attach to a detached daemon" framing → supervisor-holds-everything +
  read-only live client; the unimplemented daemon-reaper → an in-supervisor
  reclaim + startup reconcile over the user DB.

## Testing strategy

The supervisor/discovery/pty-ownership layer is the **highest-priority test
target** (it carries the risk tmux's hardening would otherwise absorb):
"client disconnects/crashes mid-session, agent ptys keep running, reconnect
recovers state"; "supervisor restarts, in-flight agents are cleanly reaped or
reclaimed via recorded PID/pty state"; "second supervisor refuses to start";
"stale socket after crash is reclaimed". Plus the never-block watchdog
(inject a permission-prompt pattern → agent is killed+reclaimed, never blocks)
and spend-cap enforcement end-to-end. E2E splits into CI-feasible (fake/cassette
agents, no spend) vs local-developer-only (real claude/codex/opencode under a
spend cap).
