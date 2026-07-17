# IPC Drive+Observe API — Design

**Goal:** Extend the supervisor's local IPC surface from the read-only-TUI
shape it has today into a versioned **drive + observe** API, so a second client
(the coming Fyne GUI) can not just watch but *act*: import a plan, pause/resume
a plan, approve a task awaiting approval, and kill a running worker — all as
first-class calls on the same socket the TUI already uses.

**Status:** design. Author: agent (full-autonomy desktop-app mandate,
2026-07-17). Builds on the merged supervisor architecture (AGENTS.md) and the
onboarding spec.

## Why

The IPC surface today is: `status`, `attach` (read-only event stream),
`enqueue` (wake dispatch), `stop`, `reload-config`. That is exactly what a
read-only cockpit needs. A GUI that lets a human *control* agents needs verbs
the TUI never did — and those verbs must be:

- **Versioned**, so a newer GUI talking to an older supervisor (or vice-versa)
  fails cleanly with a clear "unsupported command / version mismatch" instead
  of a confusing decode error. The wire protocol has no version field today.
- **On the same socket** — no new transport, no new discovery. The supervisor
  stays the single authority; the GUI is just another dumb client.

## Versioning

Add a protocol version to the wire handshake:

- `Request` gains an optional `proto_version int` (default 0 = pre-versioned,
  treated as v1 for back-compat with the current TUI client).
- The supervisor advertises its supported version in `StatusReply.ProtoVersion`.
- A `Request` naming a command the supervisor doesn't implement returns a
  structured `Response{Ok:false, Error:"unsupported command: <cmd>", Code:
  "unsupported_command"}` rather than a generic failure, so a client can
  detect capability by trying + catching, or by reading `StatusReply`.
- `Response` gains an optional `code string` — a stable machine-readable error
  class (`unsupported_command`, `not_found`, `conflict`, `invalid_args`) so the
  GUI can react programmatically instead of string-matching `Error`.

Current `CmdEnqueue`/`CmdStatus`/etc. keep their exact shapes; the version
field and `code` are additive, so the existing TUI client (which sends neither)
keeps working unchanged.

## New commands

Each is a thin, authenticated-by-locality call the supervisor's Handler
implements against the store + orchestrator. None bypass the control invariant:
killing a worker still goes through the same kill-and-reclaim path.

| Command | Args | Reply | Store/orch call |
|---|---|---|---|
| `plan-import` | `{markdown, slug?, title?}` | `{plan_id, slug, title}` | `store.CreatePlan` + `SetPlanStatus(active)` — the same logic `plan import` runs, moved server-side so the GUI needn't open the DB itself |
| `plan-set-status` | `{plan_id, status}` (pause\|active\|abandoned) | `{plan_id, status}` | `store.SetPlanStatus` (validated to the allowed transitions) |
| `task-approve` | `{plan_id, task_id}` | `{ok}` | transition a `ready_pending_approval` task to `ready` (an approval-clearing store method) |
| `worker-kill` | `{worker_id}` | `{ok}` | orch/supervisor kill-and-reclaim for that worker's task (mark its task pending, terminate the subprocess) |

`plan-ls` is intentionally NOT added: the GUI reads plans via the same
read-path the TUI's live data source uses (a direct store read is fine for a
local observe surface; only *mutations* need to funnel through the supervisor
so there is one writer of record for drive actions).

## Handler + client

- `ipc.Handler` gains `HandlePlanImport`, `HandlePlanSetStatus`,
  `HandleTaskApprove`, `HandleWorkerKill`. The supervisor implements them
  against its store + orchestrator (it already holds both).
- `ipc.Client` gains matching typed methods (`PlanImport`, `PlanSetStatus`,
  `TaskApprove`, `WorkerKill`) plus a `NegotiatedVersion()` helper that reads
  `StatusReply.ProtoVersion`.
- The `plan import` CLI is refactored to call the new IPC command WHEN a
  supervisor is reachable (single writer), falling back to the current direct-
  store path only when offline — so CLI and GUI share one code path server-side.

## Safety / invariants

- **Locality is the auth boundary** (unchanged): the socket is 0600 under a
  per-user dir; anyone who can connect is the user. No new auth is introduced.
- **Validation server-side**: `plan-set-status` rejects illegal transitions;
  `worker-kill` on an unknown/already-dead worker is a benign no-op returning
  `code:"not_found"`, not an error.
- **Idempotency**: approving an already-approved task, or pausing an already-
  paused plan, returns success (the desired end state already holds).
- **No control-invariant bypass**: `worker-kill` reuses the existing
  kill-and-reclaim so a killed worker's task returns to the ready pool exactly
  as a watchdog kill would.

## Testing

- Unit tests per new Handler method against a test store (import creates+
  activates; set-status validates transitions; approve clears the gate; kill
  reclaims). 
- Client/server round-trip tests over a real bound socket (the existing
  `ipc_test.go` harness) for each command, plus a version-negotiation test
  (old-style request with no version still works; unknown command returns
  `code:"unsupported_command"`).
- A `cmd` test that `plan import` uses the IPC path when a supervisor is up and
  the direct-store path when it isn't.

## Out of scope (later)

- The GUI itself (consumes this API).
- Streaming *drive* acknowledgements beyond the single Response (attach already
  carries observation events; a killed-worker event flows through attach).
- Remote/multi-user auth — this stays a local, single-user control plane.
