# Guided First-Run Onboarding — Design

**Goal:** When a user runs `radioactive_ralph` cold on an interactive terminal —
nothing installed, no supervisor running, no project initialized — offer to set
everything up in one guided, consent-gated step instead of printing four
commands and exiting.

**Status:** design. Author: agent (under the full-autonomy onboarding mandate,
2026-07-17). Follows the merged supervisor architecture (AGENTS.md).

## Problem

Today a cold `radioactive_ralph` invocation:
1. auto-routes to `--init` (registers the project), then
2. tries `supervisor.Find`, fails, and prints:
   ```
   radioactive_ralph: no supervisor is running.
   Install the durable background service:  radioactive_ralph service install
   or run one in the foreground (dies with this terminal):  radioactive_ralph --supervisor
   ```
   and exits non-zero.

That is correct and honest, but it is a dead end for a first-time human: they
must read the message, choose between two options, run a second command, then
re-run the client. The seam for a real wizard is already flagged in
`cmd/radioactive_ralph/init_cmd.go` ("the full interactive wizard is a later
phase").

## Non-negotiable constraints

1. **Never prompt on a non-interactive stdin/stdout** (a pipe, CI, `go test`).
   The existing "print the exact commands, exit non-zero" path MUST remain
   verbatim for that case — `tests/e2e` and `cmd` tests assert it. The wizard is
   strictly gated on `tui.IsTerminal()` (which already gates the TUI) AND stdin
   being a terminal.
2. **Consent before any outward-facing action.** Installing a background service
   registers a launchd/systemd/SCM unit — a real, persistent, system-level
   change. The wizard MUST show exactly what it will create (the state-root path,
   the DB path, the service unit name + path) and get an explicit yes before
   doing it. Default to the safe choice on bare Enter is acceptable only for the
   *foreground* option, never for service install.
3. **Idempotent.** Safe to run repeatedly. If the service is already installed or
   the supervisor already running, the wizard detects that and skips ahead rather
   than erroring or double-installing.
4. **Graceful degradation.** If `service install` fails or isn't permitted
   (locked-down machine, no launchd/systemd/SCM access), fall back to offering
   the foreground `--supervisor` path, and if the user declines everything, exit
   via the existing print-commands path (still non-zero) — never leave the user
   in a half-configured state.
5. **No new heavy deps.** Reuse `service` (install/status), `supervisor`
   (Find/Acquire discovery), `store`/`xdg` (paths), and a minimal prompt helper
   (stdlib `bufio` over stdin). No survey/tui library for the wizard itself — a
   plain, scriptable Y/n/quit prompt keeps it testable and consistent.

## Flow

```
radioactive_ralph  (cold, interactive TTY)
  │
  ├─ resolve/register project (existing ensureProjectKnown → runInitMode)
  │
  ├─ supervisor.Find(stateRoot)
  │     ├─ reachable ──────────────► launch TUI (unchanged)
  │     └─ not reachable ──► FIRST-RUN WIZARD:
  │
  │   ┌─────────────────────────────────────────────────────────────┐
  │   │ "No supervisor is running yet. Ralph can set this up:"       │
  │   │   • state dir:  <xdg.StateRoot()>                            │
  │   │   • database:   <stateRoot>/state.db                         │
  │   │   • service:    <service.UnitName(backend)> (<unit path>)    │
  │   │                                                              │
  │   │  Install the background service and start it now? [Y/n/q]    │
  │   └─────────────────────────────────────────────────────────────┘
  │        │
  │        ├─ Y (default): service.Install → start → poll Find until up
  │        │       ├─ success ─────────► launch TUI
  │        │       └─ install failed ──► offer foreground fallback ↓
  │        │
  │        ├─ n: "Run a foreground supervisor in another terminal? [y/N]"
  │        │       ├─ y: print the exact `--supervisor` command, exit 0
  │        │       │      (user runs it; nothing outward-facing done)
  │        │       └─ N/q: existing print-commands path, exit non-zero
  │        │
  │        └─ q: existing print-commands path, exit non-zero
```

On a non-TTY invocation the whole wizard block is skipped and the current
print-commands-and-exit-nonzero behavior runs unchanged.

## Components

- `internal/onboard/` — a new package holding the wizard as a testable unit:
  - `type Prompter interface { Confirm(question string, defaultYes bool) (bool, error) }`
    — abstracts stdin so tests inject scripted answers; the real impl reads a
    line from stdin and interprets `y`/`n`/`q`/empty.
  - `type Plan struct { StateDir, DBPath, ServiceUnit, ServiceUnitPath string; Backend service.Backend }`
    — the "what will be created" summary, computed from `xdg` + `service`.
  - `func Run(ctx, deps) (Outcome, error)` — drives the flow above against
    injected deps (Prompter, a "find supervisor" func, an "install service" func,
    a "wait until reachable" func, an output writer). Pure orchestration, no
    direct syscalls — every side-effecting dependency is an injected function so
    the flow is unit-testable without installing a real service.
  - `Outcome` ∈ {SupervisorReady, PrintedForegroundHint, PrintedCommands,
    Declined} so the caller (client.go) knows whether to proceed to the TUI.
- `cmd/radioactive_ralph/client.go` — the `ErrNoSupervisor` branch calls
  `onboard.Run` when interactive; on `SupervisorReady` it falls through to the
  TUI; otherwise it returns the same non-zero "no supervisor listening" error as
  today (so scripts and the existing tests are unaffected).

## Testing

- Unit tests in `internal/onboard` drive `Run` with a scripted `Prompter` and
  fake dep funcs, asserting each branch: Y→install→ready; Y→install-fails→
  foreground-fallback; n→foreground; n→N→print-commands; q→print-commands. No
  real service is installed.
- A `cmd` test confirms the NON-interactive path is byte-for-byte unchanged
  (the wizard never runs; the existing message + non-zero exit).
- The real binary is driven manually (interactive) once to confirm the prompts
  render and the happy path installs + reaches the TUI — recorded in the PR.

## Explicitly out of scope (later items)

- The GUI onboarding surface (the Fyne app will reuse `internal/onboard`'s
  `Plan` + `Run` orchestration behind its own consent dialog).
- Uninstall/repair flows beyond what `service uninstall` already gives.
