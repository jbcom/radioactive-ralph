# radioactive-ralph — Supervisor Architecture (design)

Status: approved-in-brainstorm, 2026-07-16. This is the comprehensive
architecture that supersedes the current durable-service / per-repo-plandag /
committed-`.radioactive-ralph/`-dir model. It is implemented on one large
branch (`feat/supervisor-architecture`) with one comprehensive directive set,
because the coordination protocol, database location, and config model are
interdependent and cannot be split without designing the seams twice.

Rationale for every decision below is in `.agent-state/decisions.ndjson`.

**Tech stack** (no binary-size constraint; heavier deps are fine): Go 1.26;
`creack/pty` (agent pty ownership); `charmbracelet/bubbletea` + `lipgloss`
(TUI); `spf13/cobra` + `spf13/viper` (CLI + layered config — replacing kong);
`modernc.org/sqlite` (pure-Go, no CGO — the single user DB); `yuin/goldmark`
(pure-Go plan-markdown AST); `a2aproject/a2a-go` (official A2A vocabulary/types,
stdlib-only core); `adrg/xdg` + `internal/xdg` (paths). The pure-Go SQLite
choice is a **build-compatibility** decision (C loadable extensions can't load
into modernc; CGO would lose no-CGO cross-compile), *not* a size rule — it does
not restrict cobra/viper/grpc/etc.

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

Both modes share the **same initialization pipeline** — a **wizard TUI**, or
**headless flags**, or a **config passed by path** — the client for
project-level config, the supervisor for user-level config. See §5a for how
config is merged and validated, and §5b for how a project is identified.

- **`radioactive_ralph --init`** explicitly initializes (or re-initializes) a
  project.
- Plain **`radioactive_ralph`** in a directory: if the directory is not yet a
  known project in the user DB, it **auto-routes to init**; otherwise it runs.
- **`--supervisor`** makes the working directory irrelevant (it operates at the
  user/XDG level).

## 5a. Config: virtual layers, change-vs-override, conflict diffing

Configuration is never a single committed file; it resolves through virtual
layers built by the supervisor. Three override flags feed it, each a distinct
role:

- **`--config-file` / `-C`** — a joint config file; may contain a `projects:`
  stanza. The tidy single-file form.
- **`--user-config-file`** — a user-specific config file; may *also* carry a
  `projects:` stanza; loaded the same way.
- **`--project-config-file`** — config for one specific project; **ignored in
  `--supervisor` mode**.

**Two virtual layers, built in order:**

1. **Virtual USER config** (low → high precedence):
   `DB config` < `--config-file` < `--user-config-file`.
2. **Virtual PROJECTS config** (in the supervisor, per project):
   `all projects from the DB` < `projects:` stanza from the virtual USER config.

**Change vs. override — the load-bearing distinction.** When the client talks to
the supervisor it signals its **heuristics** (project fingerprints, §5b) and any
project-config **changes**:

- **CHANGES** occur via the headless/TUI wizard **or** an explicit
  **`--init`** (new or redone initialization). In this mode a
  `--project-config-file` is treated as changes: it is **both** merged on top of
  the virtual `user.projects` config for that project **and stored to the DB**.
- **OVERRIDES** occur in **normal client mode** (non-init). A passed
  `--project-config-file` signals overrides, not changes: the project keeps its
  stored initialization unmodified, and the file merges on top of the virtual
  `user.projects` config for that project **at runtime only** (not persisted).

**Supervisor conflict warning.** If project config arriving via `--config-file`
or a `--user-config-file` `projects:` stanza would **override** a stored
project's settings, the supervisor does **backwards-looking diffing** and warns
explicitly: the user must either keep passing that config as
`--project-config-file`, or remove the conflicts — and since removal is trivial
once computed, the supervisor **offers to remove them automatically**.

**Validation runs against the merged virtual layer.** If required pieces are
missing after the merge, Ralph **exits with an error reporting exactly what must
be defined** — one mechanism regardless of source (flags, wizard, or file).

**Implementation note:** `viper` does the mechanical merge (defaults < file <
env < flags) under these rules; we own the DB layer, the two-layer USER→PROJECTS
composition, the `projects:` stanza handling, the change-vs-override distinction,
and the conflict-diff. `cobra` provides the flags (`--config-file`/`-C`,
`--user-config-file`, `--project-config-file`).

## 5b. Project identity: accumulated fingerprints, not paths

Absolute-path identity is fragile (moves and renames break it). A project is
instead identified by **accumulated fingerprints** stored in the user DB:

- **Git directory**: fingerprint via git heuristics (e.g. root-commit sha,
  remote, repo-root markers).
- **Non-git directory**: seed with the absolute path as an identifier.

Identifiers **accumulate**: a non-git directory that is later `git init`-ed
transparently gains its git identifier(s) *on top of* the path identifier, so
the same project stays recognized across the git transition and across
directory moves.

## 5c. Supervisor discovery (the socket is the advertisement)

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
  **all** projects: the plan DAG state, DB-resident project config (§5a),
  project identity fingerprints (§5b), process tracking, spend accounting, and
  session/role history. Because the supervisor always runs first and always, it
  always knows "what's next" in every project's plan.
- The supervisor takes **regular backups** of this DB.
- **Repos stay clean** — zero committed directories by default. A user *may*
  opt in by authoring a project-level override config and pointing at it by
  path, or git-track their user config (documented), but it is never the
  default. This eliminates the collision/merge-conflict/repo-litter problems of
  file-based per-repo state.
- This is explicitly **not** the current `.agent-state`-skill pattern.

The XDG user-level config recommends **resource thresholds** (memory/CPU)
derived from the host's configuration.

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

Each provider profile is a **capability record**, not a persona: the binary,
how it is invoked non-interactively, how its structured result and usage/cost
are read (§3), how it resumes, and crucially whether the CLI/API **natively
supports subagents / workflows / parallelism** — a flag the orchestrator uses to
decide whether to delegate a parallel step-group to one fan-out-capable agent
rather than spawning N Ralph-managed workers (§10). Adding a provider is a
contributor-friendly, documented, table-driven registration.

## 10. No variants — one mutating Ralph; orchestrator-verified completion

**Variants are removed entirely.** There is no blue/green/savage lineup, no
persona roleplay, no `internal/variant` registry. `radioactive_ralph` is **one
persisting, flexible, mutating agent** that becomes whatever a task needs.
Parallelism and posture are **judgment calls** (headless heuristics or TUI
choices) driven by the plan's structure (§12), not by baked personas. Prompts
are minimal and situational: *"you are an agent; here is your task; here is the
necessary context"* — never *"you are an expert NodeJS developer."* This removes
the entire persona surface and simplifies every system/user prompt.

**Layering (A2A orchestrator↔worker).** By necessity a top-most Ralph — the
**team-lead / orchestrator** — runs alongside the supervisor. It reads the plan,
decides what is next (§12's heuristic decomposition), dispatches worker Ralphs
with their **plan-scoped context** (the relevant plan slice, not a giant dump),
and — critically — **verifies completion.**

**Completion is orchestrator-verified, never agent-asserted.** A step is *not*
done because a worker says so, and **especially not because a worker
terminated** (termination may be an error/crash). A worker submits **evidence of
completion** (what it ran, exit codes, output, diff); only the orchestrator
transitions the task to a terminal `done` state, after checking that evidence
against the plan's actual done-criteria. This is the correctness backbone and
the reason the A2A `TaskState` transitions (§13) are driven by *us*, not by the
worker.

**Context/liveness discipline.** Rather than detecting an agent's automatic
context compaction (hard, unreliable), the supervisor sends **periodic
enforcement prompts** ("stay on task; if you can spawn subagents/workflows, do;
otherwise self-check at the next convenient point") and **kills-and-restarts
fresh** when an agent manually hits its context end. Each worker writes its own
**decision-log markdown** in an XDG path; the team-lead **absorbs** those into
project history to inform what runs next.

**Squads (optional, capability-aware).** For a parallelizable step-group (§12),
the orchestrator may fan out multiple worker Ralphs with path/worktree isolation
— *or*, when the bound agent CLI natively supports subagents/workflows/
parallelism (a **capability flag** on the provider profile, §9), delegate the
fan-out to that agent instead of spawning N Ralph-managed workers. Cost and
progress propagate **up** the chain to the macro TUI.

## 11. Plan engine — simple markdown, heuristic decomposition (no LLM)

Plans are **markdown so simple no agent can break it**, parsed with **goldmark**
(pure-Go, MIT) into an AST and decomposed **heuristically** — no LLM, no
structured-output, no vectors (rejected as brittle *and* CGO-incompatible).

Grammar (validated by a plan-format validator):

- **A heading of level N is a nesting group.** Its "section" runs from the
  heading to the next heading of level ≤ N (goldmark headings are flat siblings;
  we own this stop-at-next-heading scan).
- **Heading order encodes group dependency**: `# Do first` then `# Do next`
  means the first group completes before the second.
- **Under a leaf heading** (one with no child subheadings): an **unordered
  list** = parallelizable steps; an **ordered list** = sequential steps; a step
  may carry paragraphs of detail (bullets/paragraphs together = one step with
  detail).
- **Do not descend past a heading that has child subheadings** — the subheadings
  carry the ordering.
- **Disambiguation rule** (validator-enforced): a list under a heading makes it a
  step-group; a bare paragraph with no list is narrative/notes, not a step.

The orchestrator computes **past / present / future** purely from the AST + the
DB's done-state: what is pending at the current group, whether it decomposes
into subgroups or parallel/sequential steps, and what to dispatch next. An
optional **plan config** (virtual-merged like §5a, dot-notation coordinates such
as `s1.1: {parallel: false}`) can assist translating user intent, but the
markdown + list-type convention covers the common case unaided.

**Planning genesis.** Turning a vague prompt into a full plan is the one
"vaguely interactive" moment, and we sidestep "how to extract questions": a team
of agents **juxtapose and challenge** each other to refine the input (inline
prompt *or* a full markdown doc) until it covers the work end-to-end. **Headless
mode → emits the final markdown plan.** **TUI mode → renders that markdown for
review** (scroll, an embedded editor, or open-in-`$EDITOR` — both offered).
Users may also **skip planning** and run their input as-is. The refined markdown
document *is* the review surface — no question-extraction machinery.

## 12. A2A coordination vocabulary (official SDK)

Agent-to-agent coordination uses the **official `a2aproject/a2a-go` SDK**
(Apache-2.0) for its **vocabulary and types** — `a2a.Task`, `a2a.TaskState`
(`Submitted`/`Working`/`InputRequired`/`Completed`/`Failed`/…), `a2a.Message`,
`a2a.AgentCard`. The `a2a/` core-types package is stdlib-only (no grpc/CGO), so
it imports cleanly into the pure-Go build; the heavier `a2asrv`/`a2agrpc`/
`a2apb` packages are pulled in only if we later expose a real network-facing A2A
surface. `a2asrv` is a set of **interfaces we implement** (`AgentExecutor`,
`AgentCardProducer`), not a server we are forced to run.

We keep **durability** (the one SQLite DB — new `a2a_tasks`/`a2a_messages`
tables, not a second store) and the **completion trust model** (§10) as ours:
`TaskStateInputRequired` is exactly the never-block signal (a worker needing
input is a *task state the orchestrator handles*, not a blocked pty), and only
the orchestrator drives the transition to `Completed`. The third-party
`a2abridge` was rejected (its packages are `internal/`-only and its tool design
lets an agent self-complete — the opposite of our trust model).

## 13. What survives, what changes

- **Survives / is vindicated**: `internal/ipc` (repurposed: attach → discover),
  `internal/runtime/flock.go` (PID lock), `internal/service` (supervisor
  distribution), `internal/xdg`, `internal/provider` (minus gemini, plus a
  capability flag and the new agent runtime), the plan DAG *model* (moves into
  the single user DB), Bubble Tea TUI.
- **Changes / is removed**: `internal/variant` (deleted — no personas); the
  **kong** CLI (→ **cobra** + **viper**); per-repo `plans.db` and the committed
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
