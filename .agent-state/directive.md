# radioactive-ralph — supervisor-architecture rewrite directive

**Status:** ACTIVE — clean-slate rewrite per docs/superpowers/plans/2026-07-16-supervisor-architecture.md

Orchestrator: this agent. Executors: chosen per-task (haiku=mechanical,
sonnet=standard impl, opus/fable=hard reasoning) via Workflow fan-outs.
Each task ends build/test-green (branch is mid-flight but every checkpoint
compiles + passes its own tests). One large branch; final PR(s) at the end.
Full decision trail: .agent-state/decisions.ndjson. Spec:
docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md.

## Phase 1 — Foundation: pty-owned agent + never-block watchdog
- [x] internal/agent/agent.go: pty-owned Agent (Start/Output/Kill/Wait/PID/Done) + tests — DONE (creack/pty direct dep)
- [x] internal/agent/watchdog.go: never-block Watchdog (Progress/Stall/Prompt/Exited) + tests — DONE
- [x] Phase 1 checkpoint: build/test/-race/golangci-lint/gofmt green; control invariant demonstrable — DONE

## Phase 2 — User store (single XDG SQLite DB)
- [ ] [WAIT-AGENT] internal/store Go layer (open XDG DB, migration runner, plandag CRUD port with PR#63 safety) — delegated to executor; schema/0001_initial.up.sql already authored
- [ ] [WAIT-AGENT] project fingerprint accumulate/resolve (projects.go) — in the store executor delegation
- [ ] [WAIT-AGENT] project_config/spend/sessions/workers CRUD (config.go, tasks.go) — in the store executor delegation
- [ ] [WAIT-AGENT] in-store reaper reclaim (reaper.go) — in the store executor delegation
- [ ] [WAIT-AGENT] backup routine + Phase 2 checkpoint verify — in the store executor delegation; verify on return

## Phase 3 — Config resolution (cobra/viper)
- [ ] [WAIT-AGENT] internal/vconfig — BLOCKED on Phase-2 store executor (consumes store.GetProjectConfig/ResolveProject); design prepped (scratchpad/phase3-vconfig-design.md), cobra/viper staged; author when store lands
- [ ] [WAIT-AGENT] vconfig two virtual layers — blocked on store (GetProjectConfig); design in scratchpad/phase3-vconfig-design.md
- [ ] [WAIT-AGENT] vconfig change-vs-override semantics — blocked on store (SetProjectConfig)
- [ ] [WAIT-AGENT] vconfig conflict-diff + validation — blocked on store; design in scratchpad
- [ ] [WAIT-AGENT] Phase 3 checkpoint — after vconfig authored (blocked on store)

## Phase 4 — Supervisor + discovery (cobra CLI, kong removed)
- [ ] internal/supervisor: --supervisor lifecycle owning agents + store
- [ ] discovery: socket-at-XDG = advertisement; connect = discover; single-instance + stale-PID reclaim (reuse internal/ipc + flock)
- [ ] dumb client: discover-or-refuse handshake; --init routing; kong->cobra/viper migration of the CLI surface
- [ ] Phase 4 checkpoint green (client refuses without supervisor; 2nd supervisor refuses; stale socket reclaimed)

## Phase 5 — Providers + detection (capability records, no personas)
- [ ] rework internal/provider onto internal/agent; claude/codex runners; opencode.go (run --format json)
- [ ] provider.Profile capability record incl. NativeFanout flag (not a persona)
- [ ] internal/agentdetect: probe PATH, classify supported/deprecated/remote/unknown + reason; distinguish cursor vs cursor-agent
- [ ] agy spike: bind only if --print confirmed local-surface
- [ ] Phase 5 checkpoint green + cassettes

## Phase 6 — Plan engine + orchestration (variants deleted)
- [ ] internal/plan: goldmark parse + stop-at-next-heading-<=N grouping; heuristic decompose (heading order=group dep, unordered=parallel, ordered=sequential, don't descend past a heading with subheadings); validator (list-vs-bare-paragraph rule)
- [ ] internal/orch: dispatch next with plan-scoped context; orchestrator-verified completion (evidence -> verify -> done, never agent-asserted)
- [ ] internal/orch lifecycle: enforcement-prompt cadence + kill/restart on manual context-end; per-agent XDG decision logs absorbed by team-lead
- [ ] internal/a2a: adopt a2aproject/a2a-go a2a.Task/TaskState/Message over user DB (a2a_tasks/a2a_messages)
- [ ] DELETE internal/variant entirely
- [ ] Phase 6 checkpoint green

## Phase 7 — TUI + planning genesis
- [ ] internal/tui: read-only macro/meso/micro (model/update/view split, NOT one god file); subscribe + DB scrollback
- [ ] internal/genesis: agent-juxtaposition refinement -> markdown; headless emits doc, TUI renders for review (scroll + embedded/\$EDITOR), skip path
- [ ] Phase 7 checkpoint green

## Phase 8 — E2E + teardown + CI
- [ ] tests/e2e fixtures from ~/src/reference-codebases/test-repo; CI-feasible cassette path + local real-agent path (env-gated + spend cap)
- [ ] DELETE dead old-model code (plandag, runtime daemon bits, kong wiring, committed-config-dir); wire or delete rlog
- [ ] docs sweep: fix drift (runbook socket-path, fabricated RequireOperatorApproval field), remove AI-trope/extraneous, regen API docs
- [ ] real-agent E2E: orchestrator dispatches workers against a real markdown plan under spend cap with live CLI-health observation
- [ ] final: go test ./... -race, golangci-lint, tox -e docs all green; open final PR(s); babysit to squash-merge

## Notes
- Just-in-time step expansion: expand each phase's TDD micro-steps against the then-current tree at phase start (recorded strategy).
- CodeQL-go fix belongs upstream in gh-fleet-sync (codeql.yml is centrally managed); branch protection already set on main.
