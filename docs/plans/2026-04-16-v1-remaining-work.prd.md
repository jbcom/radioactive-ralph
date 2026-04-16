---
title: V1 remaining work — launch hardening, platform validation, and post-merge polish
lastUpdated: 2026-04-16
orphan: true
---

# V1 Remaining Work PRD

This document captures the remaining work after the repo-service runtime,
provider-layer, Windows IPC, and Sphinx-docs pivots.

The product is now structurally coherent:

- one binary: `radioactive_ralph`
- one durable repo-scoped runtime: `service start`
- one attached bounded runner: `run --variant <name>`
- one cockpit: `tui`
- one repo-visible config surface: `.radioactive-ralph/`
- one durable plan store: `plans.db`

What remains is mostly launch hardening, native-host validation, and operator
surface polish.

## 1. Current baseline

As of 2026-04-16, the repo already ships:

- repo-scoped durable runtime over Unix sockets and Windows named pipes
- provider bindings for `claude`, `codex`, and `gemini`
- repo-local plan gating, approvals, handoffs, retries, and task history
- Windows SCM integration and Unix service-manager integration
- Sphinx docs rooted at `docs/`
- generated Go API docs under `docs/api/`
- opt-in hosted workflows for service-manager smoke and live-provider smoke

The remaining work is not a missing architecture rewrite. It is the work
required to make that architecture launch-ready and operationally boring.

## 2. Goals

1. Finish the current PR and merge the repo-service runtime cleanly.
2. Prove the runtime on real native hosts, not only hermetic CI and
   cross-compilation.
3. Close the last operator-surface gaps so the TUI and CLI feel complete.
4. Turn the current provider abstraction into a stable launch contract.
5. Leave archival/history material clearly separated from the live product.

## 3. Non-goals

This PRD does not reopen the following:

- MCP as a core runtime dependency
- plugin packaging as the primary product surface
- a return to Astro/Starlight for docs
- a new architecture split between multiple executables
- reintroducing Python-era daemon, multiplexer, or skill-wrapper behavior

## 4. Workstreams

## 4.1 P0 — Finish the current PR and launch gate

### Problem

The current runtime branch is functionally ready, but it should not be squash
merged until the hosted PR signal is clean and the remaining launch-critical
checks are explicitly accounted for.

### Tasks

1. Get PR `#40` to a clean hosted state.
   - required checks green
   - no unresolved review threads
   - no draft-only caveats left in the PR description

2. Record the exact Windows CI contract.
   - Unix runners keep `go test -race ./...`
   - Windows runner keeps `go test ./...`
   - Windows behavior is additionally covered by:
     - native CLI smoke
     - temp-repo smoke
     - live repo-service IPC smoke

3. Decide whether the current hosted Windows lag is runner instability or a
   persistent repo problem.
   - if repo problem: fix it before merge
   - if runner issue: capture it explicitly in the PR notes and proceed only
     when the rerun result is clean

### Acceptance criteria

- PR `#40` is ready for review, not draft
- all required checks are green on the final pre-merge run
- unresolved review threads count is `0`

## 4.2 P0 — Native host validation

### Problem

The repo now has strong hermetic coverage, but the remaining launch risk is on
real host-manager and real provider environments.

### Tasks

1. Run `.github/workflows/service-managers.yml` on real native runners.
   - macOS launchd install/start/stop
   - Linux systemd user-unit install/start/stop
   - Windows SCM install/start/stop

2. Run `.github/workflows/provider-live.yml` with real credentials.
   - Claude smoke
   - Codex smoke
   - Gemini smoke

3. Capture findings from those runs into docs and follow-up issues.
   - if clean: document them as validated launch criteria
   - if not clean: file concrete defects and fix launch blockers before tagging

### Acceptance criteria

- one clean native service-manager run per supported platform
- one clean credentialed provider run for each shipped provider
- launch docs updated with any real-host caveats discovered

## 4.3 P0 — Release readiness

### Problem

The runtime is close to launchable, but the release story still needs one
deliberate pass across packaging, installation, and rollout expectations.

### Tasks

1. Perform a dry run of the release toolchain.
   - GoReleaser build
   - Homebrew formula path
   - Scoop manifest path
   - Chocolatey package path
   - curl installer path

2. Verify install docs against actual release output.
   - macOS install
   - Linux install
   - Windows install

3. Validate post-install operator flow.
   - `radioactive_ralph init`
   - `radioactive_ralph doctor`
   - `radioactive_ralph service start`
   - `radioactive_ralph tui`
   - `radioactive_ralph run --variant fixit --advise`

### Acceptance criteria

- every documented install path matches the shipped artifact names and commands
- the release job can produce the expected binaries and package metadata
- there is one written release checklist in the repo or release notes process

## 4.4 P1 — TUI completion

### Problem

The TUI is now real and useful, but it is not yet the full operator cockpit the
product wants to be.

### Tasks

1. Add task-detail and plan-detail drilldown.
   - current task status
   - variant/provider
   - recent event history
   - dependency and acceptance summary

2. Add direct in-TUI operator actions.
   - approve
   - requeue
   - retry
   - handoff
   - mark done
   - mark failed

3. Add filtering and navigation polish.
   - queue-by-status views
   - provider/variant filters
   - keyboard help overlay
   - clearer event rendering

4. Add service/session visibility.
   - active provider runs
   - worker/session state
   - queue counts by status
   - last-event and failure summaries

### Acceptance criteria

- every CLI operator action has an equivalent TUI action
- the TUI can handle normal execution flow without dropping back to CLI for
  common operator tasks
- the TUI remains socket-backed, not a second runtime

## 4.5 P1 — Provider maturity

### Problem

The provider layer is real, but only Claude has richer session semantics and
the long-term “any compatible CLI provider” story is still mostly architectural
rather than fully declarative.

### Tasks

1. Decide the v1 provider contract boundary.
   - what must be code-defined
   - what can be config-defined
   - what remains provider-specific implementation

2. Strengthen non-Claude providers.
   - Codex CLI behavior under real usage
   - Gemini CLI behavior under real usage
   - better error/structured-output handling

3. Decide whether session-resume support for Codex/Gemini is in v1 or deferred.
   - if in v1: implement and test
   - if deferred: document stateless-turn behavior explicitly

4. Design the next-step declarative provider binding model.
   - executable
   - args template
   - model mapping
   - effort mapping
   - system/developer prompt injection
   - output schema parsing

### Acceptance criteria

- the docs accurately describe which providers are stateful vs stateless
- live-provider smoke proves the current three-provider contract
- there is a concrete follow-on design for broader provider configurability

## 4.6 P1 — Launch docs and runbooks

### Problem

The core docs are now aligned, but launch operators still need tighter runbooks
for installation, service management, provider auth, and failure recovery.

### Tasks

1. Add explicit operator runbooks.
   - install + first-run
   - provider auth/setup
   - service install vs service start
   - stop/recover/clean stale state
   - approval and handoff workflow

2. Add real troubleshooting pages.
   - stale heartbeat
   - dead service socket / dead named pipe
   - provider CLI missing or unauthenticated
   - service-manager install errors

3. Expand platform-specific notes.
   - launchd caveats
   - systemd-user caveats
   - Windows SCM and named-pipe caveats

### Acceptance criteria

- an operator can install, start, recover, and stop Ralph using only the docs
- docs clearly separate launch-critical guidance from archival design history

## 4.7 P2 — Archive and lore cleanup

### Problem

The live docs are correct enough, but some variant and history material still
contains lore-heavy or historical language that could confuse a first-time
operator.

### Tasks

1. Continue narrowing archival trees.
   - `reference/`
   - archived site prototype under `site/`
   - history-heavy plan documents

2. Keep live variant pages grounded in the current runtime contract.
   - current mode support
   - current provider behavior
   - current safety floor behavior

3. Clearly label historical artifacts.
   - “historical plan”
   - “archival prototype”
   - “not part of the live runtime”

### Acceptance criteria

- a new user cannot reasonably confuse the archive for the live product
- variant docs remain flavorful without overstating implementation

## 5. Execution order

## Immediate

1. finish PR `#40`
2. get the final hosted CI signal clean
3. squash merge
4. sync `main`

## Next

1. run service-manager manual workflow
2. run live-provider manual workflow
3. fix any real-host defects
4. perform release dry-run

## After launch gate

1. complete TUI operator actions and drilldown
2. harden provider maturity and docs
3. continue archive/lore cleanup

## 6. Risks

### Hosted CI instability on Windows

The repo now distinguishes between:

- actual Windows runtime defects
- PowerShell smoke-script defects
- hosted-runner instability in long Windows test jobs

This needs to be managed deliberately so the project does not keep
rediscovering the same class of CI noise.

### Real-host service-manager surprises

launchd, systemd user units, and Windows SCM are all supported in code, but
their true launch behavior still needs real-host confirmation.

### Provider drift

Provider CLIs are external dependencies and their auth or output behavior can
shift. The launch docs and manual-provider workflow need to stay current.

## 7. Definition of done

The remaining v1 work is complete when all of the following are true:

- the repo-service runtime PR is merged cleanly
- real-host manager smoke has passed for macOS, Linux, and Windows
- live-provider smoke has passed for Claude, Codex, and Gemini
- release packaging paths are dry-run validated
- the TUI covers the common operator loop without major CLI fallbacks
- launch docs match the real install, auth, runtime, and recovery flow

At that point, remaining work becomes ordinary product iteration rather than
launch gating.
