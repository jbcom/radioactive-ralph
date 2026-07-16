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
- [x] internal/supervisor lifecycle — DONE (9 tests; old model torn out; whole repo green)
- [x] supervisor discovery — DONE (9 tests; old model torn out; whole repo green)
- [x] dumb client — DONE (9 tests; old model torn out; whole repo green)
- [x] Phase 4 checkpoint — DONE (9 tests; old model torn out; whole repo green)

## Phase 5 — Providers + detection (capability records, no personas)
- [x] rework internal/provider — DONE (green; agy=not-local, deferred codex rework noted)
- [x] provider capability record — DONE (green; agy=not-local, deferred codex rework noted)
- [x] internal/agentdetect — DONE (green; agy=not-local, deferred codex rework noted)
- [x] agy spike — DONE (green; agy=not-local, deferred codex rework noted)
- [x] Phase 5 checkpoint — DONE (green; agy=not-local, deferred codex rework noted)

## Phase 6 — Plan engine + orchestration (variants deleted)
- [x] internal/plan: goldmark heuristic decomposition + validator — DONE (25 tests, green)
- [ ] [WAIT-AGENT] internal/orch: dispatch + orchestrator-verified completion — DESIGN authored (scratchpad/phase6b-orch-design.md); execute after internal/plan lands
- [ ] [WAIT-AGENT] internal/orch lifecycle: enforcement-prompt cadence + kill/restart on manual context-end; per-agent XDG decision logs absorbed by team-lead
- [ ] [WAIT-AGENT] internal/a2a: adopt a2aproject/a2a-go a2a.Task/TaskState/Message over user DB (a2a_tasks/a2a_messages)
- [x] internal/variant deleted (Phase 4) — VERIFIED gone
- [ ] [WAIT-AGENT] Phase 6 checkpoint green — verify on return

## Phase 7 — TUI + planning genesis
- [ ] [WAIT-AGENT] internal/tui: read-only macro/meso/micro (model/update/view split, NOT one god file); subscribe + DB scrollback
- [ ] [WAIT-AGENT] internal/genesis: agent-juxtaposition refinement -> markdown; headless emits doc, TUI renders for review (scroll + embedded/\$EDITOR), skip path
- [ ] [WAIT-AGENT] Phase 7 checkpoint green

## Phase 8 — E2E + teardown + CI
- [ ] [WAIT-AGENT] tests/e2e fixtures from ~/src/reference-codebases/test-repo; CI-feasible cassette path + local real-agent path (env-gated + spend cap)
- [ ] [WAIT-AGENT] DELETE dead old-model code (plandag, runtime daemon bits, kong wiring, committed-config-dir); wire or delete rlog
- [ ] [WAIT-AGENT] docs sweep: fix drift (runbook socket-path, fabricated RequireOperatorApproval field), remove AI-trope/extraneous, regen API docs
- [ ] [WAIT-AGENT] real-agent E2E: orchestrator dispatches workers against a real markdown plan under spend cap with live CLI-health observation
- [ ] [WAIT-AGENT] final: go test ./... -race, golangci-lint, tox -e docs all green; open final PR(s); babysit to squash-merge

## Phase 9 — Docs TOTAL realignment (the whole docs/ tree describes the dead model)

- [ ] DELETE docs/variants/ entirely (11 files — variants are gone; no personas)
- [ ] DELETE the committed .radioactive-ralph/plans/ dir (violates clean-repo; state lives in the user DB now)
- [ ] Rewrite README.md + AGENTS.md + CLAUDE.md to the supervisor architecture (one binary, --supervisor + dumb client, one user DB, local-only providers, no variants, markdown plans)
- [ ] Rewrite docs/getting-started + docs/guides + docs/design + docs/reference to the new model (supervisor/discovery, config virtual-layers, plan engine, orchestrator-verified completion, A2A vocabulary)
- [ ] Rewrite docs/runbooks (fix the socket-path drift + fabricated RequireOperatorApproval field flagged in review; supervisor install/attach)
- [ ] Regenerate docs/api/ via gomarkdoc against the NEW packages (agent/store/vconfig/supervisor/provider/agentdetect/plan/orch/a2a)
- [ ] Remove AI-design-trope / extraneous docs (adjective soup, over-explained obvious, marketing filler); every doc matches code
- [ ] tox -e docs builds clean; no residual mention of variant/kong/plandag/per-repo-config/durable-daemon

## Notes
- [ ] [WAIT-AGENT] Interim multi-dimensional review (code-quality/security/architecture) of committed Phases 1-6a — running; fold Critical findings in immediately, High/Medium into Phase 8.
- Just-in-time step expansion: expand each phase's TDD micro-steps against the then-current tree at phase start (recorded strategy).
- CodeQL-go fix belongs upstream in gh-fleet-sync (codeql.yml is centrally managed); branch protection already set on main.
