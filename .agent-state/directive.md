# radioactive-ralph — build-out directive

Durable work queue for the full top-to-bottom completion mandate
(2026-07-16). One work unit = one branch = one PR, babysat to a green
squash-merge. Execution model: this agent orchestrates; Sonnet 5 executors
run inside `Workflow()` fan-outs for parallelizable sub-work. Decisions and
their rationale land in `decisions.ndjson` (commit-body `Decision:`/`Why:`).

## Standing decisions (see decisions.ndjson for full rationale)

- **Nomenclature is A2A-aligned.** There is no "mayor protocol" in the
  industry; the pattern is hierarchical **orchestrator → worker**
  (supervisor/worker). Agent-to-agent coordination aligns with **A2A**
  (Agent2Agent; task = id + lifecycle + updatable state, which the plandag
  task model already matches); **MCP** covers tools/context. The planning
  variant is an *orchestrator agent* coordinating *worker variants*.
- **Local-only provider set: `claude`, `codex`, `opencode`** (+ **`agy`**
  pending U3 spike). "Local-only" means *no delegation to a cloud control
  surface* — the CLI must own the agent loop, tool execution, and session
  control locally. Hosted model inference is fine.
  - **Drop `gemini`** — CLI deprecated 2026-06-18, auth endpoint returns
    410 Gone; every invocation fails. Remove as a shipped provider.
  - **Drop `cursor-agent`** — delegates the agent session to Cursor's cloud
    surface (cloud-agent handoff). `cursor` is the editor, not an agent CLI.
  - **`opencode`** qualifies via its local `opencode run` path only. Never
    bind `opencode serve`/`web`/`--share`/attach (cloud/remote surfaces).
  - **`agy` (Antigravity CLI, gemini's Go successor)** is a local-provider
    *candidate*: installed, working (`agy models`/`agents`), claude-shaped
    surface (`--print`, `--model`, `--mode`, `--continue`). Spike it in U3;
    if its `--print` path is confirmed local-surface (no cloud project in
    the loop), add it as the 4th provider (the Google-lineage slot).
  - Detection still *recognizes* gemini/cursor-agent and explains why they
    are not offered.

## Work units

### [ ] U1 — Remove gemini as a shipped provider
Delete the gemini built-in provider (`internal/provider/gemini.go`,
`config.DefaultGeminiProvider`, registry references, run.go `--variant`
help text, docs) and any test fixtures that assume it. Keep the declarative
path so a self-hosted gemini-compatible CLI could still be wired via
config, but it is no longer a shipped/default provider. Update the provider
count/lists in docs. One commit.

### [ ] U2 — Add opencode as a built-in local provider
New `internal/provider/opencode.go` binding the local `opencode run` path:
`opencode run -m <provider/model> --format json` (stream-json events),
`--variant <effort>`, `--session`/`--continue` for resume, working dir via
`--dir`/cwd. Parse usage/cost from `opencode stats` (or the json event
stream) into `provider.Usage`. Add `config.DefaultOpencodeProvider`,
register it, add it to `shippedProviderBinaries`. Cassette tests mirroring
the claude/codex ones. Document the binding in provider-contract + a
runbook.

### [ ] U3 — Agent CLI detection + first-run heuristics
New `internal/agentdetect` (or extend `internal/doctor`): probe PATH for
known agent CLIs, capture `--version` signatures, classify each as
supported/deprecated/remote-delegating/unknown, and produce a suggested
provider configuration. `radioactive_ralph init` and a new
`radioactive_ralph agents detect` surface it. Distinguish `cursor`
(editor) from `cursor-agent`. Explain non-offered CLIs. Feeds U7's TUI
approval flow.

### [ ] U3a — Variant lineup audit (first-principles, A2A-aligned)
The agentic space changed substantially in the past year; the current 10
variants (blue, grey, green, red, professor, fixit, immortal, savage,
old-man, world-breaker) predate the A2A orchestrator/worker consolidation.
Audit ALL variants from first principles: which are genuinely distinct,
which are redundant (overlapping isolation/termination/tool posture with a
sibling), which should be dropped, which need to align with modern A2A
orchestrator↔worker roles. Produce a decision doc
(`docs/design/variant-lineup-2026.md`) with a keep/merge/drop/reshape verdict
per variant + rationale, and record each verdict in decisions.ndjson. This
audit sets the lineup that U4 (Fixit↔Professor loop) and U5 (contributor
profiles) build on, so it runs FIRST among the variant units. Use a
Workflow fan-out: one Sonnet executor per variant proposing a verdict, then
a synthesis pass reconciling overlaps against the A2A role model.

### [ ] U4 — Fixit↔Professor durable self-correcting planning loop
Evolve `fixit` and `professor` into two halves of one durable, self-
correcting planning capability (NOT a new standalone orchestrator profile).
Fixit = advisor/decomposer: scans, ROI-scores, writes the plan DAG,
recommends peers. Professor = plan→execute→reflect executor. Close the loop:
Fixit decomposes+recommends → Professor executes with reflection →
reflection feeds back to Fixit to re-plan / self-correct. This *is* the
A2A orchestrator↔worker pattern, realized over the existing plandag task
lifecycle (already A2A-shaped: task = id + lifecycle + updatable state). Do
NOT invent a parallel coordination store. Depends on U3a's audit outcome
for the exact variant boundaries. Docs page + tests demonstrating the loop
self-correcting on a fixture problem.

### [ ] U5 — Make variant/provider profiles contributor-friendly
Extract the profile-authoring surface into a documented, minimal contract:
a `docs/contributing/adding-a-variant.md` and
`docs/contributing/adding-a-provider.md`, a scaffold command or template,
and a table-driven registry so a new profile is one file + one register
call. Ensure `Profile.Validate()` messages are actionable. Own the R&D:
document, per provider, exactly how radioactive-ralph speaks to it
(argv, framing, resume, usage parsing) in `docs/design/provider-*`.

### [ ] U6 — E2E fixtures from the reference repo
Cloned https://github.com/zpqrtbnk/test-repo.git to
~/src/reference-codebases. Build a local-E2E fixture generator that seeds a
temp git repo with real, varied default files (multiple languages/configs)
for Ralphs to work on. Fixtures live under `tests/e2e/fixtures/`
(committed) with a generator that materializes a temp working repo.

### [ ] U7 — Local E2E harness with TUI approval + agentic TUI control
The big one. A local-only E2E harness that: (a) detects agents (U3),
(b) presents a suggested config for **TUI approval**, (c) supports
**agentic control of the TUI** (a scripting/automation surface so the test
harness — or an agent — can drive the socket-backed cockpit
deterministically), (d) sets up a temp repo (U6), (e) runs tasks across all
Ralphs in a logical order, culminating in the orchestrator variant (U4)
decomposing a problem and managing worker variants while the harness
observes CLI health. Split into CI-feasible (no real agent spend; fake/
cassette providers) vs local-developer-only (real claude/codex/opencode).
Gate the real-agent E2E behind an env flag + spend cap.

### [ ] U8 — Test coverage closes review Phase-3 gaps
Implement the highest-risk untested paths surfaced by review Phase 3
(durable dispatch gate enforcement end-to-end, the heartbeat reaper if it's
still unimplemented, cross-process concurrent claims under -race, spend
accumulation/restore round-trips, config binary trust at the boundary).
Fold in the specific findings from `.full-review/03-testing-documentation.md`.

### [ ] U9 — Docs sweep: no drift, no AI-trope, no extraneous
Apply review Phase-3/4 documentation findings: fix every doc that
contradicts code, delete extraneous/AI-generated-trope docs (adjective
soup, over-explained obvious things, docs that exist only because "there
should be docs"), fill genuine gaps. Every Ralph variant documented and
matched to a local test. gomarkdoc API reference regenerated.

### [ ] U10 — Best-practices + CI/DevOps hardening
Apply review Phase-4 findings (idiomatic Go, goroutine lifecycle, the
remaining high/medium findings from the earlier phases not yet fixed:
IPC frame bounds + protocol versioning, god-object decomposition of
service.go/tui.go, the orphaned internal/db task model, event-log
retention, 500ms attach polling, TUI store-reopen churn). CI: wire the
CI-feasible E2E into a workflow; ensure the real-agent E2E is documented
for local devs.

### [ ] U11 — Final consolidated verification
Full `go test ./... -race`, `golangci-lint run`, `tox -e docs`, run every
Ralph variant locally against a fixture repo, screenshot/verify the TUI,
and confirm the orchestrator-variant demo works end to end with real local
agents under a spend cap. Update `.full-review/05-final-report.md` closure
status.

## Sequencing notes

U1→U2 first (provider set correct before anything binds it). U3 (detection,
incl. the `agy` spike) depends on U1/U2. **U3a (variant audit) runs before
U4/U5** — it sets the lineup. U4 (Fixit↔Professor loop) and U6 (fixtures)
can then run in parallel. U7 depends on U3+U4+U6. U8/U9/U10 fold in review
findings (Phases 3-5 landing in `.full-review/`). U11 is the closing gate.
Each unit is its own branch + PR; open the PR once per unit at the end and
babysit to a green squash-merge.
