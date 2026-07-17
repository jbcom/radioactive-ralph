# Fyne GUI Client — Design

**Date:** 2026-07-17
**Status:** Approved (user direction 2026-07-17; architecture decisions delegated to the agent under the full-autonomy mandate)
**Related:** [supervisor-architecture design](2026-07-16-supervisor-architecture-design.md) §7 (dumb client), [IPC drive-API design](2026-07-17-ipc-drive-api-design.md)

## Motivation

The supervisor is a headless core; the TUI (`internal/tui`) is a read-only
"dumb client" on its socket. Per user direction (2026-07-17): for *watching and
controlling* long-running local AI agents, a GUI is the better primary surface
for a human than a terminal — so Ralph should ship a real desktop application,
not only a TUI. Open-source does not mean we skimp on the convenient thing.

Two hard constraints shape everything below:

1. **Go-native, not Rust.** The GUI uses **Fyne** (`fyne.io`), so the whole
   product stays one Go module with no second toolchain, no CGO-Rust bridge, and
   no serialization boundary the desktop client has to redefine — it links the
   same `internal/store` and `internal/ipc` types the TUI does.
2. **CONSISTENT, not native.** We do not want per-OS native look-and-feel. We
   want *one visual identity* — Ralph's identity — so the app feels like OUR
   application whether the user is in a terminal or on the desktop. The GUI
   mirrors the TUI's semantic palette (`internal/tui/styles.go`) and its
   macro→meso→micro drill model, rendered with Fyne widgets and a custom theme.

## Non-goals

- **No rearchitecture of the supervisor or store.** A GUI is just another client
  on the same socket. The supervisor already treats every connection identically.
- **No new IPC surface.** The GUI drives via exactly the v2 commands the
  drive-API PR added (`plan-import`, `plan-set-status`, `task-approve`,
  `worker-kill`) and observes via the same reads the TUI uses. If the GUI needs a
  capability the protocol lacks, that is a protocol change made in `internal/ipc`
  first, not a GUI-private back channel.
- **No embedded provider control.** The GUI never spawns or talks to an agent
  CLI directly. Every action is a supervisor call; the supervisor remains the
  single writer of record.
- **No auth / multi-user.** Same trust model as the TUI: a local user talking to
  their own user-level supervisor over a `0700` socket dir. No network surface.

## Architecture

```
                 ┌──────────────────────────────────────┐
                 │            Supervisor (core)          │
                 │   owns pty, store writes, dispatch    │
                 └──────────────────────────────────────┘
                        ▲  Unix socket / named pipe  ▲
        one-shot dial   │  (ipc.Client, v2 drive)    │  one-shot dial
                        │                            │
        ┌───────────────┴──────────┐      ┌──────────┴──────────────────┐
        │  internal/tui  (TUI)     │      │  internal/gui  (Fyne GUI)    │
        │  DataSource (read-only)  │      │  Controller (read + drive)   │
        │  Bubble Tea, lipgloss    │      │  Fyne widgets, ralph theme   │
        └──────────────────────────┘      └─────────────────────────────┘
             shared: internal/store types, internal/ipc client/types,
                     internal/orch.Progress, semantic status palette
```

The GUI is a peer to the TUI: same socket, same store types, same drill model.
The only structural difference is that the GUI's data seam is **read + drive**
(it can approve/pause/kill/import), whereas the TUI's `DataSource` is read-only.

### Package layout — `internal/gui`

| File | Responsibility |
|---|---|
| `controller.go` | The `Controller` interface: every read the GUI needs (mirrors `tui.DataSource`) **plus** the four drive actions. This is the seam a fake implements in tests. |
| `live.go` | `liveController` — the production `Controller`. Read methods forward to a fresh short-lived `*ipc.Client` (via `supervisor.Find`) and the shared `*store.Store`, exactly as `tui.liveDataSource` does. Drive methods forward to the `*ipc.Client` v2 typed methods (`PlanImport`/`PlanSetStatus`/`TaskApprove`/`WorkerKill`). |
| `theme.go` | `ralphTheme` — a `fyne.Theme` mapping Ralph's semantic palette to Fyne color names, so the desktop app reads as the same product as the terminal. True-color hex equivalents of the TUI's ANSI palette. |
| `status.go` | `statusColor(status string) color.Color` — the GUI twin of `tui.statusStyle`, one switch covering every real plan/task status. Single source so a status can never render as undifferentiated gray by accident. |
| `app.go` | `Run(Opts) error` — builds the `fyne.App`, the system tray/menubar item, and the main window; owns the refresh ticker and the live event subscription; wires the `Controller` into the views. |
| `macro.go` | The macro view: project-wide status header (worker/task/plan counts from `StatusReply`) + the plan list. Selecting a plan drills to meso. |
| `meso.go` | The meso view: one plan's task list with per-task status, progress bar (`orch.Progress`), and the drive affordances (approve a `ready_pending_approval` task, pause/resume/abandon the plan). |
| `micro.go` | The micro view: one task's event timeline (`ListTaskEvents`) + the kill affordance for its running worker. |
| `tray.go` | Tray/menubar glue: a compact status summary in the menu, "Open Ralph" to raise the window, "Quit". Uses `fyne.App`'s `SetSystemTrayMenu` where the desktop driver supports it; degrades to a plain window when it does not. |

`Run` and the views never touch `*ipc.Client`, `*store.Store`, or the socket
directly — they go through `Controller`. That is the enforcement seam: the fake
`Controller` in tests exercises every view and every action with zero I/O, the
same way `tui.fakeDataSource` does for the TUI.

### Controller interface

```go
// Controller is every read the GUI renders plus every drive action it can
// take. Read methods mirror tui.DataSource so the two clients share a mental
// model; drive methods map 1:1 onto the ipc v2 drive commands. A fake
// implementation drives the view tests with no supervisor and no store.
type Controller interface {
    // --- reads (mirror tui.DataSource) ---
    Status(ctx context.Context) (ipc.StatusReply, error)
    ListPlans(ctx context.Context, projectID string) ([]store.Plan, error)
    PlanProgress(ctx context.Context, planID string) (orch.Progress, error)
    ListTasks(ctx context.Context, planID string) ([]store.Task, error)
    ListProjectEvents(ctx context.Context, projectID string, limit int) ([]store.Event, error)
    ListTaskEvents(ctx context.Context, planID, taskID string, limit int) ([]store.Event, error)
    Attach(ctx context.Context, fn func(json.RawMessage) error) error

    // --- drive (ipc v2) ---
    ImportPlan(ctx context.Context, args ipc.PlanImportArgs) (ipc.PlanImportReply, error)
    SetPlanStatus(ctx context.Context, planID, status string) error
    ApproveTask(ctx context.Context, planID, taskID string) error
    KillWorker(ctx context.Context, workerID string) error
}
```

`liveController` embeds the same `runtimeDir`/`store`/`orch`/`projectID` fields
`tui.liveDataSource` has, and adds one thing: drive calls need a live client, so
each dials fresh via `supervisor.Find`, invokes the typed v2 method, and closes —
identical lifecycle to the read path. When no supervisor is reachable, drive
methods return a clear "no supervisor running" error the view surfaces as a
banner (it never silently no-ops).

### Consistency: the shared identity

The single most load-bearing rule of this design. The TUI already defines the
product's semantic palette in `internal/tui/styles.go`:

| Meaning | TUI (ANSI) | GUI (true-color) | Applies to |
|---|---|---|---|
| accent (headers/selection) | `39` blue | `#268bd2` | window chrome, selected row |
| good / done / healthy | `42` green | `#2aa198` | `done` |
| running / active | `81` cyan | `#22b2b2` | `running` |
| warn / needs attention | `214` orange | `#cb8b1a` | `blocked`, `ready_pending_approval`, `paused`, `failed_partial` |
| bad / failed | `203` red | `#dc322f` | `failed`, `abandoned` |
| muted / not-started | `244` gray | `#839496` | `pending`, `ready`, `draft`, `skipped`, `decomposed`, `archived` |

`internal/gui/status.go` reproduces `tui.statusStyle`'s switch exactly — same
cases, same buckets — so a plan or task carries the same color meaning in both
clients. A regression test asserts every `store.PlanStatus*`/`store.TaskStatus*`
constant maps to a non-default bucket in both the TUI and the GUI, so the two
palettes can never silently drift apart.

### Refresh + live updates

Same model as the TUI: a periodic refresh tick (1s) re-fetches `Status` plus the
current drill level's data, and a long-lived `Attach` subscription pushes event
frames so the view feels live between ticks. Fyne is not Elm-architecture, so
instead of TUI messages the GUI holds observable state the widgets bind to:
the refresh goroutine and the attach goroutine update that state on Fyne's main
thread via `fyne.Do` (Fyne's UI-thread marshaller), and the bound widgets
redraw. Cancellation: closing the window cancels the root context, which unblocks
`Attach` (the client trips its read deadline on ctx-done, exactly as the TUI's
attach does) and stops the ticker.

### Entry point / how it launches

`radioactive_ralph gui` is a new cobra subcommand that calls `gui.Run`. It
reuses the same startup path as the TUI: resolve `xdg.StateRoot()`, open the
shared store, resolve the current project id, then hand a `liveController` to
`Run`. If no supervisor is reachable at launch, the GUI opens anyway in a
"waiting for supervisor" state (it polls `Status` on the refresh tick and lights
up when one appears) — a GUI that refuses to open when the background service is
briefly down would be worse than the TUI, which already tolerates this.

The desktop-application packaging (the `.app`/`.dmg`, MSI, `.deb`/AppImage that
puts this in `/Applications`, Program Files, etc.) is the **next** directive
item; this spec covers the client itself and its `gui` subcommand. `fyne package`
metadata (icon, bundle id) is produced here so the packaging item consumes it.

## Testing strategy

The Fyne project ships `fyne.io/fyne/v2/test`, a headless test driver that
renders widgets without a display, so views and interactions are testable in CI
with no X server (with CGO enabled and GL/X11 headers present — see Build-tag
isolation below).

1. **`status_test.go`** — every real plan/task status maps to the intended color
   bucket; the GUI switch and the TUI switch agree bucket-for-bucket (the
   anti-drift test).
2. **`controller` fake** — an in-memory `Controller` (like `tui.fakeDataSource`)
   returning scripted plans/tasks/events and recording drive calls.
3. **View tests** (`test.NewApp()`): macro renders the status header + plan list
   from a fake; selecting a plan drills to meso; meso renders tasks + progress;
   the approve button on a `ready_pending_approval` task calls
   `Controller.ApproveTask` with the right ids; pause/resume/abandon call
   `SetPlanStatus`; micro renders the task timeline and the kill button calls
   `KillWorker`. Assert on the recorded drive calls, not on pixels.
4. **`liveController` wiring test** — start a real supervisor (as
   `plan_cmd_test.go` does), build a `liveController`, and assert a read
   (`Status`) and a drive (`ImportPlan`) both round-trip through the socket. This
   is the one test that exercises real I/O; the rest use the fake.
5. **Launch smoke** — `gui.Run` with a fake controller and the headless driver
   starts, renders the initial macro view, and shuts down cleanly on context
   cancel (no leaked goroutines — checked with a goroutine-count guard like the
   TUI's attach-leak test).

### Build-tag isolation (load-bearing — verified, not assumed)

Fyne is a **CGO** dependency: even its widget/`test` packages fail to compile
with `CGO_ENABLED=0` (they depend on build-tagged files that need CGO), and on
Linux it needs the system GL/X11 dev headers. This collides head-on with the
repo's CI, which cross-builds `go build ./...` with `CGO_ENABLED=0` across six
GOOS/GOARCH pairs. A plain `internal/gui` package importing Fyne would turn the
entire build matrix red. (Probed directly: `CGO_ENABLED=0 go vet` on a Fyne
program fails to build; the headless `test` driver runs fine *with* CGO on.)

So the whole GUI is isolated behind a `gui` build tag:

- Every file in `internal/gui` that imports Fyne, and the `gui` cobra command's
  registration, carry `//go:build gui`. The default build (`go build ./...`,
  CGO off) never compiles them, so the six-way matrix stays exactly as green as
  today.
- The `gui` cobra subcommand is registered in a `//go:build gui` file; a
  `//go:build !gui` stub registers a same-named command that prints "this build
  has no GUI support — rebuild with `-tags gui`" and exits nonzero, so the CLI
  surface is identical either way and `--help` always lists `gui`.
- The Fyne-free half of the package — the `Controller` interface, the
  `statusColor` switch, and its anti-drift test — has **no** Fyne import and no
  tag, so `status_test.go` (the TUI↔GUI palette-agreement test) runs in the
  normal CGO-off test job. Only the view/theme/app code that touches Fyne is
  tagged.
- A dedicated CI job (`gui`) runs on ubuntu + macOS with `CGO_ENABLED=1`, the
  Linux GL/X11 dev packages installed (`libgl1-mesa-dev xorg-dev`), and
  `go test -tags gui ./internal/gui/...` using the headless `test` driver — no
  display server needed. This is the job that gates the view tests, the
  liveController wiring test, and the launch smoke.

The packaging directive item (signed `.app`/MSI/`.deb`) builds `-tags gui` with
CGO on per platform; it is the only place the GL stack must link for a real
window, and it is out of scope here.

## Decision record

- **Fyne over Tauri/Wails/Rust:** user directive — stay Go-native, no second
  toolchain, link the store/ipc types directly. (decisions.ndjson)
- **Consistent over native:** user directive — one Ralph identity across TUI and
  GUI; a custom `fyne.Theme` mirroring the TUI palette, not the OS theme.
- **`Controller` extends the read seam rather than reusing `tui.DataSource`
  verbatim:** the GUI needs the four drive methods the read-only interface must
  not have (keeping the TUI's read-only guarantee intact means not adding writes
  to its interface). The read half is deliberately identical so the two stay
  mentally aligned; the anti-drift palette test enforces the visual half.
- **Tray + full window, both:** user directive ("tray/menubar + full window").
  The tray is the ambient always-there affordance; the window is the workspace.
```
