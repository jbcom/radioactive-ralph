---
title: AGENTS.md — radioactive-ralph
lastUpdated: 2026-07-26
---

# Extended Agent Protocols — radioactive-ralph

Read `CLAUDE.md` first for the core shape, and the authoritative design at
`docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md`. Its
native Windows pty and service clauses are superseded by
`docs/superpowers/specs/2026-07-26-windows-scm-safety-disable-design.md`.

## Product contract

radioactive-ralph is one binary that runs in two modes:

1. **`radioactive_ralph --supervisor`** — the long-lived supervisor: owns
   every agent subprocess's pty, holds all work open, serves the control
   socket, runs the reaper, and owns the one user-level SQLite DB.
2. **`radioactive_ralph`** (no flag) — the **dumb client**: discovers the
   supervisor via its socket, initializes project config, and renders a
   read-only view. It refuses to run without a supervisor.

Native Windows supports only the foreground supervisor/client control plane:
SCM install/start and provider-backed worker ptys are disabled. WSL2 with the
Linux build and `systemd --user` is the functional Windows provider route.

Do not describe the product as a Claude plugin, an MCP server, or a family of
slash-command skills. There are **no variants/personas** — that model was
removed. There is one mutating Ralph; behavior comes from the plan and the
task context, not a persona.

## The control invariant (non-negotiable)

An agent CLI must NEVER block the system — no permission prompts, no
clarification waits, no interactive menus. Agents run non-interactively under
Ralph's own pty (`internal/agent`, via `creack/pty`); the watchdog surfaces
any stall/prompt/no-output and the runtime auto-resolves, denies, or
kills-and-reclaims. Kill is cheap because state is durable (the plan slice in
the DB), so recovery is replaying that slice to a fresh worker.

## State — one user-level SQLite DB, clean repos

All project/plan/config/worker/spend state lives in **one user-level SQLite
DB** under the XDG state root (`internal/store`, opened by the supervisor).
There is NO committed repo state — no `.radioactive-ralph/` dir, no per-repo
DB. Projects are identified by accumulated fingerprints (git root-commit +
remote + abs-path), so identity survives `git init` and directory moves.
Never store runtime state under `.claude/`.

## Command surface

- `radioactive_ralph --supervisor` — run the supervisor.
- `radioactive_ralph` — dumb client (discover + read-only TUI).
- `radioactive_ralph gui` — desktop GUI client: a graphical peer to the TUI on
  the same socket that can also drive (approve/pause/kill/import). Present in
  GUI-enabled builds (`-tags gui`); a bare desktop launch (double-clicked
  bundle, no TTY) opens it automatically.
- `radioactive_ralph --init` — initialize/re-initialize the current project.
- `radioactive_ralph plan import <file>` — import a markdown plan and activate
  it (the supervisor's periodic dispatch loop then drives its ready steps).
- `radioactive_ralph plan ls [--all]` — list the current project's plans.
- `radioactive_ralph events [--backlog N] [--json]` — tail the current project's
  live supervisor events to stdout (the headless peer of the TUI/GUI live view;
  the CLI consumer of the Attach observe API). `--backlog N` prints the N most
  recent events first; `--json` emits JSONL.
- `radioactive_ralph doctor` — environment checks.

A plan step opts into MECHANICAL, orchestrator-verified completion with an
inline marker: `` `accept: <shell command>` `` (re-run; must exit 0) and/or
`` `accept-file: <path>` `` (must exist in the project checkout). A step with
no marker is judgment-only (accepted on non-empty worker evidence). Completion
is never inferred from a worker terminating — the orchestrator re-checks.

Config resolves through virtual layers (`internal/vconfig`, cobra/viper):
three flags (`--config-file`/`-C`, `--user-config-file`, `--project-config-file`),
USER layer = DB < `--config-file` < `--user-config-file`, PROJECTS layer =
all-DB-projects < the user config's `projects:` stanza.

## Providers — local-only capability bindings

Shipped providers: `claude`, `codex`, `opencode`. "Local-only" means the CLI
owns the agent loop + tool execution + session control locally (hosted model
inference is fine). `gemini` removed (CLI deprecated 2026-06-18);
`cursor-agent` excluded (delegates the session to Cursor's cloud). Each
provider binding is a **capability record** (`internal/provider/binding.go`),
including a `NativeFanout` flag for CLIs that natively fan out subagents/
workflows — NOT a persona. Detection/classification is `internal/agentdetect`.
A2A coordination vocabulary is the official `a2aproject/a2a-go`.

## Plans + completion

Plans are simple markdown decomposed heuristically over the goldmark AST
(`internal/plan`): heading = group, unordered list = parallel steps, ordered =
sequential, don't descend past a heading with subheadings. A step ending in the
`[approval]` marker is held (`ready_pending_approval`) until an operator approves
it, gating dispatch of that step — see docs/guides/plan-format.md. No LLM in
decomposition. The orchestrator (`internal/orch`) dispatches steps with
plan-scoped context and **verifies completion against acceptance criteria** —
completion is never agent-asserted and never inferred from termination.

## Self-test (dogfooding)

`scripts/self-test.sh` imports `docs/plans/self-test.md` into a running
supervisor and has Ralph verify Ralph. Every step carries an inline
`accept: <command>`, so the orchestrator RE-RUNS the check itself rather than
accepting a worker's claim -- build, unit, race, lint, then e2e and the repo
claim verifier, wired with `after:` edges so the three fast checks fan out in
parallel once the build passes.

Two things made the first attempt useless, both worth not repeating:

- The plan lived in `.radioactive-ralph/plans/`, which is gitignored because
  the product contract forbids committed repo state. A branch switch deleted
  it. The plan is now a tracked SOURCE file under `docs/plans/`; the state it
  produces still goes to the user-level DB, so the contract holds. A plan file
  is an input like a Makefile, not runtime state.
- No step carried an `accept:` marker, so every task was judgment-only --
  accepted on non-empty evidence, failed on empty. Nothing was verified and the
  whole plan died. A dogfooding plan without acceptance markers tests nothing.

Reading `radioactive_ralph status` during a self-test run is also the fastest
way to exercise the operator surface, because a live run is the only thing that
produces running workers, fan-out partitions, and real provenance at once.

## Testing patterns

- `go build ./...` must compile; `go test ./...` for the main pass;
  `go test -race ./...` for concurrency-touching packages.
- `golangci-lint run` for lint (gofmt-clean; the repo's gci convention merges
  third-party + internal imports into one block after stdlib).
- `python3 -m tox -e docs` for the docs build.
- Run `bash scripts/generate-api-docs.sh` when the exported Go API changes.
- Each new package lands build/test/-race/lint-green in isolation (the rewrite
  proceeds phase-by-phase per the implementation plan).
- **A check that finds nothing to check must FAIL, not pass.** Silent no-op and
  genuine success print identically, so a green result proves nothing about
  whether anything was examined. This is the most repeated mistake in this
  repo's history, and it recurs because it wears a different costume at each
  layer.

  **The check itself.** `go test -run <typo>` printed `ok` for ZERO tests.
  `verify-repo-claims.sh` guard 9 reported "no merged PRs listed as open" twice
  while matching nothing -- once because the list used bare numbers with no `#`
  to extract, once because the label read "open PR" and the pattern required
  "open PRs". Guard 8's count went to zero when its checkbox pattern stopped
  matching, printing "0 open items", which reads as an empty queue. A shell loop
  echoed "pushed" after a REJECTED push, because the `echo` ran unconditionally.
  Both verifier guards now fail loudly on an empty match.

  **The check's SETUP.** A DeletePlan test asserted no orphaned `events`
  remained, but its fixture never ran a task, so the plan had no events -- the
  assertion passed while the delete really did leave every row behind (2 before,
  2 after). An absence assertion must first prove PRESENCE and fail if the
  pre-state is empty.

  **The check's THRESHOLD.** Three timing bounds for one guard failed three
  ways: 2s absolute passed locally at 26ms and failed CI at 2.074s; a "doubling
  costs <= 8x" ratio PASSED against the restored quadratic code (both shapes are
  super-linear, 6.6x vs 3.3x, and runner noise cannot separate them); 500ms
  absolute failed CI at 2.15s -- the FIXED code on the runner was slower than
  the QUADRATIC code on the dev machine, so no absolute threshold distinguishes
  the shapes across hardware. Assert algorithmic SHAPE instead: `EXPLAIN QUERY
  PLAN` shows a `MATERIALIZE`d CTE versus a `CORRELATED SCALAR SUBQUERY`,
  identically on every machine. Name the query as a constant so a test can
  EXPLAIN it.

  When writing a check, ask what it prints if its input format changes. If that
  output is indistinguishable from success, the check is decorative.

  **The rule applies to the check's own code, and to how you verify it.** A
  first attempt to break guard 8 rewrote only top-level `- [ ]` and left an
  indented sub-item, so the count was 1 rather than 0 and the guard correctly
  stayed quiet -- which LOOKED like the guard failing to fire. The test was
  incomplete, not the guard. Separately, a tracked-edit guard shipped three bugs
  in a row -- an undefined variable borrowed from another script, a baseline
  captured after staging, a comparison blind to an already-dirty file -- each
  found by exercising it rather than reading it. A guard is code: restore the
  defect and watch it FAIL before trusting it. A negative result is evidence
  only once the setup is confirmed to produce the condition being tested.
- **A new task field is not shipped until every surface renders it.** The
  observe DTO feeds three renderers (TUI meso, GUI meso, CLI `status`), and
  landing a field in one is the most repeated mistake in this repo's history --
  three times in a single session: provenance, the terminal-blocker, and the
  durable failure category each shipped to one surface first and had to be
  chased into the others. The asymmetry is invisible to a green suite, because
  each surface's tests only know about that surface.
  When adding a field, write the per-renderer test at the same time, and put
  any DISPLAY POLICY (which values are worth showing, how they are abbreviated)
  in `observe` rather than in a renderer -- `PartitionLabels`, `ProvenanceLabel`
  and `WorkerSuffix` are all there because a second copy drifted or was about to.
- **Prove a fix by reverting it.** A test that passes after a change may have
  passed before it. Re-apply the defect and confirm the named test fails for
  the stated reason; if it does not, the test is not testing the fix.

  **The revert is itself a check, so it needs the same scepticism.** Three
  "negative proofs" in one session reported a clean PASS/FAIL while the
  mutation had NOT been applied: a `/tmp` baseline overwritten by a later
  step, a `grep` treating `*` in `30 * time.Second` as a glob, and a string
  anchor that silently matched nothing. Each printed a confident result about
  code that was never modified. Mutate by LINE NUMBER, print the mutated line
  to confirm it landed, and confirm it still COMPILES -- a build error means
  the test never ran at all.

  **If reverting changes nothing, the branch may be unreachable.** A PTY
  cleanup fix survived deleting the fix entirely, because on an idle host
  `kern.proc.all` drops every member within one poll (2 members before the
  kill; 0 at +0ms, +2ms, +20ms), so the guarded deadline branch never
  executed. Documenting that gap in a comment is not closing it. Add a seam --
  an injectable package-level var -- so the branch runs deterministically.
  Writing the test that finally executed it found two further bugs in that
  same fix, including a predicate that dropped a pid on ANY lookup error and
  would have reported cleanup as clean while a worker kept running.

  **One case matching N patterns proves none of them.** A prompt-detector
  suite stayed green when a whole pattern was deleted, because every positive
  example also matched two other patterns. Assert the isolation property
  directly: each positive case must match exactly one pattern.
- **Docs claims need the same proof as code claims — EACH of them.** A guide
  written this session asserted four mechanisms that do not exist: that a
  contained turn sets HOME/TMPDIR under the containment root (nothing in
  internal/ sets either), that acceptance commands run in their own scratch
  trees (verify.go only sets cmd.Dir), that a `pN` partition means one provider
  turn (codex is NativeFanout: false, so those tasks ran as separate workers),
  and a `status` snapshot that could not be emitted at all -- `build done`
  beside `e2e cannot run: build failed`, two runs stitched together.
  I had verified the three GREPPABLE facts and reported the guide as checked.
  The invented ones were all explanatory prose, which is where a plausible
  mechanism hides: nothing looks wrong about a sentence describing how
  something works. Paste real captured output rather than composing an example,
  and grep for the symbol behind every "because X does Y".
- **Verify the ARTIFACT, not the intent.** "I made the edit" is not "the file
  changed"; "the import succeeded" is not "the run reflects current code". Both
  gaps shipped false claims in one session and neither was visible without
  going back to look:
  - A scripted replacement targeted a string that did not match, so the edit
    silently did nothing. The build passed because nothing had changed, and the
    commit message described a fix that was not in the code. A REVIEWER caught
    it; re-reading the file would have.
  - The self-test reported on a stale plan (6 stored tasks vs 12 in the file)
    after an import that "succeeded". The mechanism worked; the outcome was
    wrong, and only inspecting the stored run showed it.
  After any scripted or multi-step edit, grep the file for the new text before
  claiming it landed. After any state-changing command, read back the state --
  not the command's exit code.
- **Read the output the user gets, not the assertions about it.** Assertions
  test what you thought to check; a rendered view or printed line has
  properties nobody wrote an assertion for. Four defects in one session were
  invisible to a fully green suite and obvious on sight:
  - every unrun task in `status` carried TRAILING WHITESPACE, because the
    status column is padded for marker alignment and an unrun task has no
    markers to fill it.
  - the widest meso row was 215 columns. It wrapped on any terminal, and the
    wrap landed mid-sentence so the markers scattered.
  - abbreviating a worker id from the FRONT rendered every row `worker-…`;
    ids share a constant head, so the marker correlated nothing. No width
    assertion could catch it — the broken version was exactly as narrow.
  - the GUI's blocked-reason and partition markers had NO test at all: the
    fake controller deals in `store.Task`, so `Blocked` was never populated
    on the one path that renders it, and the markers could have been deleted
    with the suite still green.

  Dump the real bytes (`t.Logf` the rendered view, `cat -A` the CLI output)
  and look. Two corollaries, both learned the hard way here:
  - **the dump must show everything the operator sees.** A first widget walker
    covered `Button` and `Label` but not `canvas.Text`, and reported a row as
    having no status at all. The walker was wrong, not the view.
  - **when a fix and a test disagree, check which one is right.** Truncating
    descriptions to fit the width broke a live E2E test asserting an ordinary
    40-character description is visible. The test was right; the fix was
    wrong, and the markers — not the description — had to give way.

## Adding a command / provider / package

1. CLI commands: add a cobra command under `cmd/radioactive_ralph/`, back it
   with logic in the relevant `internal/` package.
2. Providers: add a runner + a capability `BindingConfig` and register it in
   `internal/provider` (table-driven); document how Ralph speaks to it.
3. Keep `internal/store` a leaf; the supervisor sits on top; the client is
   dumb. No import cycles.

## PR workflow

- Work on branches; merge through GitHub PRs; prefer squash merges.
- Keep `main` tracking `origin/main` exactly; branch protection requires
  Test/Lint/Build + conversation resolution.
- Resolve review threads and keep CI green before merge.
