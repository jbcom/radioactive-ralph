# radioactive-ralph — supervisor-architecture rewrite directive

**Status:** ACTIVE — rewrite merged (v0.10.0), but NOT done. "Done" = every gap we identified + everything comprehensive-review / UI-review / security / simplification / bug-hunt digging surfaces is resolved. Now driving a full multi-lens audit of merged main and fixing everything real it finds, then the desktop-app + onboarding effort.

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
- [x] internal/orch: dispatch — DONE (21 tests; verified-completion proven; no grpc)
- [x] internal/orch lifecycle — DONE (21 tests; verified-completion proven; no grpc)
- [x] internal/a2a: adopt — DONE (21 tests; verified-completion proven; no grpc)
- [x] internal/variant deleted (Phase 4) — VERIFIED gone
- [x] Phase 6 checkpoint — DONE (21 tests; verified-completion proven; no grpc)

## Phase 6c — Close tech debt NOW (no deferral — hidden gaps are bad practice)

- [x] Wire agent.Watch END-TO-END — DONE (tested; invariant enforced; no inert scaffolding)
- [x] Implement NativeFanout — DONE (tested; invariant enforced; no inert scaffolding)
- [x] Rework codex runner — DONE (tested; invariant enforced; no inert scaffolding)
- [x] Wire vconfig.DiffConflicts — DONE (tested; invariant enforced; no inert scaffolding)
- [x] Wire supervisor HandleEnqueue — DONE (tested; invariant enforced; no inert scaffolding)
- [x] Phase 6c checkpoint — DONE (tested; invariant enforced; no inert scaffolding)

## Phase 7 — TUI + planning genesis
- [x] internal/tui: read-only — DONE (23 tests; verified by running the real binary)
- [x] internal/genesis: agent-juxtaposition — DONE (23 tests; verified by running the real binary)
- [x] Phase 7 checkpoint — DONE (23 tests; verified by running the real binary)

## Phase 8 — E2E + teardown + CI
- [x] tests/e2e fixtures + CI-feasible + live paths — DONE (Phase 8)
- [x] DELETE dead old-model — DONE (Phase 8 complete; 3x-reliable E2E; go1.26.4; service+rlog wired)
- [x] docs sweep — DONE (Phase 8 complete; 3x-reliable E2E; go1.26.4; service+rlog wired)
- [x] real-agent E2E — DONE (Phase 8 complete; 3x-reliable E2E; go1.26.4; service+rlog wired)
- [x] final: — DONE (Phase 8 complete; 3x-reliable E2E; go1.26.4; service+rlog wired)

## Phase 9 — Docs TOTAL realignment (the whole docs/ tree describes the dead model)

- [x] DELETE docs/variants/ (11 files) — DONE
- [x] DELETE the committed .radioactive-ralph/ dir — DONE
- [x] Rewrite README.md + AGENTS.md + CLAUDE.md — DONE (all three realigned to the supervisor architecture)
- [x] Rewrite docs/getting-started + docs/guides + docs/design + docs/reference to the new model (supervisor/discovery, config virtual-layers, plan engine, orchestrator-verified completion, A2A vocabulary)
- [x] Rewrite docs/runbooks (fix the socket-path drift + fabricated RequireOperatorApproval field flagged in review; supervisor install/attach)
- [x] Regenerate docs/api/ via gomarkdoc against the NEW packages (agent/store/vconfig/supervisor/provider/agentdetect/plan/orch/a2a)
- [x] Realign the SITE landing (site/ Astro: RalphHero.astro + any component referencing variants/personas/durable-service) to the supervisor model
- [x] Update Sphinx config/nav (docs/conf.py, docs/index.md toctree, docs/_static) so the PUBLISHED site (jonbogaty.com/radioactive-ralph via cd.yml) reflects the new architecture; verify the site build (site/ pnpm build) + Sphinx build both clean
- [x] Remove AI-design-trope / extraneous docs (adjective soup, over-explained obvious, marketing filler); every doc matches code
- [x] tox -e docs builds clean; no residual mention of variant/kong/plandag/per-repo-config/durable-daemon

- [x] Babysit PR #73 to green squash-merge — DONE. Root-caused + fixed all CI portability failures (macOS sun_path socket-path fallback; Windows pty boundary; three Windows CI-workflow-script bugs: `$home` reserved var, PID-lock share-mode + shutdown-timeout flake, empty-ArgumentList binding) and all 6 CodeRabbit P1 gaps (plan import/ls CLI, tick-driven dispatch, project-checkout worker cwd, real `accept:` acceptance, config-backed binding resolver, cobra SilenceErrors). Also renamed the stuttering `--radioactive_ralph-bin` flag to `--bin`. All threads resolved; squash-merged as 00c788d.

- [x] Babysit PR #75 (post-merge API-doc regen + RELEASED flip) — DONE, squash-merged (a3532c2). release-please auto-cut v0.10.0 (#74).

## Full multi-lens audit of merged main (the real "done" bar)
- [x] post-release-audit workflow — DONE (55 agents, 7 lenses, adversarially verified): 29 confirmed findings (3 high, 13 medium, 13 low). Saved to scratchpad/audit-findings.json. Fixing all on branch chore/post-release-audit.
  - [x] Fix 3 HIGH — DONE (394a55a): MarkFailed owner guard + ErrTaskNotOwnedRunning; acceptLoop log-and-continue on transient errors; CreateTask ErrDuplicateTask sentinel so real insert failures surface. Regression tests each.
  - [x] Fix 13 MEDIUM — DONE (cccca9d + 775c94d + 4376a2a): ipc read-deadline + Attach ctx-cancel; agent back-pressure blocking-send; heartbeat-interval single-source; opencode doctor check; abs_path clock-driven ordering (fixed a real latent bug — added_at wasn't clock-driven); plan-import honesty; TUI empty-state; 4 docs-accuracy fixes; plan-CLI + Acquire-live-but-unresponsive coverage.
  - [x] Fix 13 LOW — DONE (775c94d + 1305f3c): fanout task release on mid-loop error; per-uid socket-dir perms; dead resolveModel branch; recorder mutex snapshot; retry-exhaustion coverage; doctor `run`→`service install` tagline; no-supervisor hint→service install; TUI status colors + newline polish.
  - [x] Full gate green (build/vet/test/-race/lint 0 issues) + real binary verified end-to-end.
  - [x] Babysit PR #76 (29 audit fixes) — DONE, squash-merged (e8268db). CodeRabbit's second-scrutiny found 6 real issues in the fixes themselves (incomplete agent-leak guard, cancel-without-deadline hang, INSERT-OR-IGNORE not refreshing added_at on move-back, retry-budget penalty for system aborts, socket-dir pre-creation attack) — all fixed + threads resolved. Then a real regression my socket-hardening introduced (over-strict dir check rejected 0755 CI tempdirs) — rescoped to Ralph's own rralph-<uid> leaf only, pinned with tests.
  - [x] Second-pass audit (workflow w6ctqzxux) — DONE: 14 confirmed of 15 raw (3 high, 6 medium, 5 low), several introduced BY the earlier fixes + one serious pre-existing bug (pty-echo watchdog false-kill) the first pass missed. All 14 fixed with regression tests on chore/audit-second-pass. Highs: MarkDone/MarkBlocked owner-guard symmetry; pty DisableEcho.
  - [x] Babysit PR #79 (14 second-pass fixes) — DONE, squash-merged (f8b3de2). Caught + fixed my own linux termios mistake (TIOCGETA is BSD-only; split echo_linux.go/echo_bsd.go) + CodeRabbit's runCtx-scope medium. release-please cut v0.10.1 (#78).
  - [ ] Third convergence pass on #79's fix surface (the ~10 files it changed). Stop the audit loop when a pass confirms ~zero new findings. Then desktop-app + onboarding.

## NEXT EFFORT (own directive + branch, starts after this one closes) — desktop app + onboarding
The supervisor is a headless core; the TUI is a dumb client on its socket. A GUI is just another client on the same socket — no rearchitecture. User direction (2026-07-17): build a Go-native GUI (Fyne), CONSISTENT not native — one visual identity across terminal and desktop, feels like OUR app either way; ship a real desktop application (.app in /Applications, installer into Program Files, .desktop/AppImage on Linux), because for watching/controlling agents a GUI is the better primary surface for humans. Open-source ≠ limits.
- [ ] [WAIT] (blocked on audit closing) Guided first-run onboarding: when a user runs `radioactive_ralph` cold (no service installed, no supervisor, no user DB), a TTY-gated wizard OFFERS to set it all up in one guided step — `service install` (creates XDG state root + the one user-level SQLite DB + native launchd/systemd/SCM unit and starts it), then `--init` the current project, then the TUI. Constraints: never prompt on non-TTY/CI (keep the current "print exact commands, exit nonzero" path — tests assert it); show exactly what will be created + get consent before installing a background service (outward-facing action); offer a foreground `--supervisor` fallback when service install isn't permitted; fully idempotent. Seam already flagged in cmd/radioactive_ralph/init_cmd.go ("interactive wizard is a later phase").
- [ ] [WAIT] (blocked on audit closing) Harden the IPC into a versioned local DRIVE+observe API (today it's shaped for the read-only TUI; the GUI needs to approve/pause/kill/import as first-class calls).
- [ ] [WAIT] (blocked on audit closing) Fyne GUI client (tray/menubar + full window per later design decision), peer to the TUI on the same socket; shares store/ipc types directly (no serialization boundary to redefine).
- [ ] [WAIT] (blocked on audit closing) Real native installers/packaging: signed+notarized .app/.dmg (macOS), MSI/MSIX into Program Files (Windows), .deb/.rpm/AppImage + .desktop (Linux). Audit the existing site/public/install.sh against the cosign signatures goreleaser already produces; add goreleaser nfpms (deb/rpm) + winget.

## Notes
- PR #73 review-absorption (commit 84161bb + a8102be): fixed the CI portability failures at the root (ipc.ServiceEndpoint short-path socket fallback for the macOS sun_path 104-byte limit; agent.ErrPTYUnsupported + WSL boundary on Windows; launchd/reclaim tests made host-portable) AND all 6 CodeRabbit P1 gaps (plan import/ls CLI, tick-driven dispatch, worker/acceptance run in the project checkout via store.ProjectAbsPath, real acceptance derived from `accept:`/`accept-file:` plan markers, config-backed binding resolver, cobra SilenceErrors). All 6 review threads resolved. Docs updated (README/AGENTS/getting-started). Full local gate green + real binary verified end-to-end (init→plan import→plan ls).
- [x] Interim review of Phases 1-6a — DONE (found+fixed agent Kill double-close; doc-comment fixes; dead-code removal)
- Just-in-time step expansion: expand each phase's TDD micro-steps against the then-current tree at phase start (recorded strategy).
- CodeQL-go fix belongs upstream in gh-fleet-sync (codeql.yml is centrally managed); branch protection already set on main.
