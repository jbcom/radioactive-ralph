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
- [x] internal/store Go layer — DONE (28 tests, green)
- [x] project fingerprint — DONE (28 tests, green)
- [x] project_config/spend — DONE (28 tests, green)
- [x] in-store reaper — DONE (28 tests, green)
- [x] backup routine — DONE (28 tests, green)

## Phase 3 — Config resolution (cobra/viper)
- [x] internal/vconfig — DONE (16 tests, green)
- [x] vconfig two virtual — DONE (16 tests, green)
- [x] vconfig change — DONE (16 tests, green)
- [x] vconfig conflict — DONE (16 tests, green)
- [x] Phase 3 checkpoint — DONE (16 tests, green)

## Phase 4 — Supervisor + discovery (cobra CLI, kong removed)
- [ ] [WAIT-AGENT] internal/supervisor lifecycle (owns agents + store) — delegated to Phase-4 executor
- [ ] [WAIT-AGENT] supervisor discovery (socket-at-XDG, single-instance, stale reclaim) — delegated to Phase-4 executor
- [ ] [WAIT-AGENT] dumb client discover-or-refuse + kong->cobra/viper CLI migration — delegated to Phase-4 executor
- [ ] [WAIT-AGENT] Phase 4 checkpoint (client refuses w/o supervisor; 2nd supervisor refuses; stale reclaim) — verify on return

## Phase 5 — Providers + detection (capability records, no personas)
- [ ] [WAIT-AGENT] rework internal/provider onto internal/agent; claude/codex runners; opencode.go (run --format json) — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] provider.Profile capability record incl. NativeFanout flag (not a persona) — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] internal/agentdetect: probe PATH, classify supported/deprecated/remote/unknown + reason; distinguish cursor vs cursor-agent — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] agy spike: bind only if --print confirmed local-surface — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] Phase 5 checkpoint green + cassettes — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)

## Phase 6 — Plan engine + orchestration (variants deleted)
- [ ] [WAIT-AGENT] internal/plan: goldmark parse + stop-at-next-heading-<=N grouping; heuristic decompose (heading order=group dep, unordered=parallel, ordered=sequential, don't descend past a heading with subheadings); validator (list-vs-bare-paragraph rule) — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] internal/orch: dispatch next with plan-scoped context; orchestrator-verified completion (evidence -> verify -> done, never agent-asserted) — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] internal/orch lifecycle: enforcement-prompt cadence + kill/restart on manual context-end; per-agent XDG decision logs absorbed by team-lead — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] internal/a2a: adopt a2aproject/a2a-go a2a.Task/TaskState/Message over user DB (a2a_tasks/a2a_messages) — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] DELETE internal/variant entirely — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] Phase 6 checkpoint green — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)

## Phase 7 — TUI + planning genesis
- [ ] [WAIT-AGENT] internal/tui: read-only macro/meso/micro (model/update/view split, NOT one god file); subscribe + DB scrollback — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] internal/genesis: agent-juxtaposition refinement -> markdown; headless emits doc, TUI renders for review (scroll + embedded/\$EDITOR), skip path — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] Phase 7 checkpoint green — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)

## Phase 8 — E2E + teardown + CI
- [ ] [WAIT-AGENT] tests/e2e fixtures from ~/src/reference-codebases/test-repo; CI-feasible cassette path + local real-agent path (env-gated + spend cap) — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] DELETE dead old-model code (plandag, runtime daemon bits, kong wiring, committed-config-dir); wire or delete rlog — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] docs sweep: fix drift (runbook socket-path, fabricated RequireOperatorApproval field), remove AI-trope/extraneous, regen API docs — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] real-agent E2E: orchestrator dispatches workers against a real markdown plan under spend cap with live CLI-health observation — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)
- [ ] [WAIT-AGENT] final: go test ./... -race, golangci-lint, tox -e docs all green; open final PR(s); babysit to squash-merge — gated on the running Phase-2 store executor (whole rewrite is sequenced behind it)

## Notes
- Just-in-time step expansion: expand each phase's TDD micro-steps against the then-current tree at phase start (recorded strategy).
- CodeQL-go fix belongs upstream in gh-fleet-sync (codeql.yml is centrally managed); branch protection already set on main.
