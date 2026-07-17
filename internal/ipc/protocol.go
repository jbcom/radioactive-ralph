// Package ipc is radioactive-ralph's repo-service IPC layer.
//
// The repo service listens on a local control-plane endpoint under the
// repo's state directory: a Unix domain socket on macOS/Linux and a
// named pipe on Windows. `radioactive_ralph status`,
// `radioactive_ralph attach`, `radioactive_ralph stop`, and internal
// control-path clients exchange newline-delimited JSON messages over
// the same transport.
//
// Heartbeat liveness is signalled via the repo service touching an
// `.alive` file every few seconds. `radioactive_ralph status` checks the file's
// mtime before even attempting a socket connect — if the service
// crashed and left a stale socket, we want to surface the dead-service
// state cleanly rather than hang on a connection attempt.
//
// Wire protocol:
//
//	Request:  {"cmd": "<verb>", "args": {...}}\n
//	Response: {"ok": true|false, "data": ..., "error": "..."}\n
//
// For commands that stream (attach), the server sends N >= 0 frames
// of {"event": {...}}\n followed by a terminating {"ok": true}\n.
package ipc

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProtoVersion is the wire protocol version this build speaks. The original
// read-only-TUI surface (status/attach/enqueue/stop/reload-config) is v1; the
// drive commands (plan-import/plan-set-status/task-approve/worker-kill) are v2.
// A client omitting Request.ProtoVersion is treated as v1 for back-compat.
const ProtoVersion = 2

// Command names for the JSON-line protocol.
const (
	// v1 — observe surface.
	CmdStatus       = "status"
	CmdAttach       = "attach"
	CmdEnqueue      = "enqueue"
	CmdStop         = "stop"
	CmdReloadConfig = "reload-config"

	// v2 — drive surface (see the IPC drive-api design spec).
	CmdPlanImport    = "plan-import"
	CmdPlanSetStatus = "plan-set-status"
	CmdTaskApprove   = "task-approve"
	CmdWorkerKill    = "worker-kill"
)

// Stable machine-readable error classes carried in Response.Code so a client
// (the GUI) can react programmatically instead of string-matching Error.
const (
	CodeUnsupportedCommand = "unsupported_command"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeInvalidArgs        = "invalid_args"
)

// Request is a single command from a client to the repo service.
type Request struct {
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
	// ProtoVersion is the wire version the client speaks. 0 (omitted) means a
	// pre-versioned v1 client (the current TUI), handled for back-compat.
	ProtoVersion int `json:"proto_version,omitempty"`
}

// Response is the single-shot reply shape. For streaming commands the
// server sends multiple Event frames followed by a final Response with
// Ok=true; mid-stream errors send a Response with Ok=false.
type Response struct {
	Ok    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
	// Code is a stable machine-readable error class (Code* consts) set on
	// !Ok responses where the client may want to branch on the failure kind.
	Code string `json:"code,omitempty"`
}

// StreamEvent is one frame emitted during a streaming command (e.g. attach).
type StreamEvent struct {
	Event json.RawMessage `json:"event"`
}

// StatusReply is the data payload for CmdStatus responses.
type StatusReply struct {
	// ProtoVersion is the supervisor's supported wire version, so a client
	// can detect drive-command availability without trial-and-error.
	ProtoVersion  int             `json:"proto_version,omitempty"`
	RepoPath      string          `json:"repo_path"`
	PID           int             `json:"pid"`
	Uptime        time.Duration   `json:"uptime_ns"`
	ActiveWorkers int             `json:"active_workers"`
	ReadyTasks    int             `json:"ready_tasks"`
	ApprovalTasks int             `json:"approval_tasks"`
	BlockedTasks  int             `json:"blocked_tasks"`
	RunningTasks  int             `json:"running_tasks"`
	FailedTasks   int             `json:"failed_tasks"`
	ActivePlans   int             `json:"active_plans"`
	Workers       []WorkerSummary `json:"workers,omitempty"`
	LastEventAt   time.Time       `json:"last_event_at,omitempty"`
	HeartbeatAge  time.Duration   `json:"heartbeat_age_ns,omitempty"`
}

// WorkerSummary is the runtime-facing status for one in-flight worker.
type WorkerSummary struct {
	// WorkerID is the store worker-row id — the value a client passes to the
	// worker-kill drive command to target THIS worker. Distinct from any
	// provider-session id.
	WorkerID          string `json:"worker_id"`
	PlanID            string `json:"plan_id"`
	TaskID            string `json:"task_id"`
	Provider          string `json:"provider,omitempty"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
}

// EnqueueArgs is the client's payload when pushing work via CmdEnqueue.
type EnqueueArgs struct {
	TaskID      string `json:"task_id"` // optional; service generates UUID if empty
	Description string `json:"description"`
	Priority    int    `json:"priority,omitempty"`
}

// EnqueueReply tells the client whether a new task was created or a
// duplicate was collapsed (via FTS dedup in the db layer).
type EnqueueReply struct {
	TaskID   string `json:"task_id"`
	Inserted bool   `json:"inserted"` // false means FTS found a duplicate
}

// StopArgs controls the termination mode for CmdStop.
type StopArgs struct {
	Graceful bool          `json:"graceful"`             // wait for in-flight sessions to finish cleanly
	Timeout  time.Duration `json:"timeout_ns,omitempty"` // overrides default if >0
}

// --- v2 drive-surface payloads ---

// PlanImportArgs imports a markdown plan and activates it (CmdPlanImport). The
// server runs the same CreatePlan + activate logic the `plan import` CLI does,
// so the GUI needn't open the DB itself and there is one writer of record.
type PlanImportArgs struct {
	Markdown string `json:"markdown"`
	Slug     string `json:"slug,omitempty"`  // optional; derived from title if empty
	Title    string `json:"title,omitempty"` // optional; derived from first heading/filename if empty
	Project  string `json:"project"`         // project id the plan belongs to
}

// PlanImportReply reports the created plan.
type PlanImportReply struct {
	PlanID string `json:"plan_id"`
	Slug   string `json:"slug"`
	Title  string `json:"title"`
}

// PlanSetStatusArgs changes a plan's lifecycle status (CmdPlanSetStatus), e.g.
// pause/resume/abandon. The server validates the transition.
type PlanSetStatusArgs struct {
	PlanID string `json:"plan_id"`
	Status string `json:"status"` // paused|active|abandoned
}

// PlanSetStatusReply echoes the applied status.
type PlanSetStatusReply struct {
	PlanID string `json:"plan_id"`
	Status string `json:"status"`
}

// TaskApproveArgs clears the approval gate on a ready_pending_approval task
// (CmdTaskApprove), transitioning it to ready so dispatch can pick it up.
type TaskApproveArgs struct {
	PlanID string `json:"plan_id"`
	TaskID string `json:"task_id"`
}

// WorkerKillArgs kills a running worker (CmdWorkerKill) via the same
// kill-and-reclaim path a watchdog kill uses, so the task returns to ready.
type WorkerKillArgs struct {
	WorkerID string `json:"worker_id"`
}

// OKReply is the trivial success payload for drive commands that only need to
// confirm the action landed.
type OKReply struct {
	OK bool `json:"ok"`
}

// encode writes v as JSON followed by a newline to buf.
func encodeJSONLine(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("ipc: marshal: %w", err)
	}
	return append(data, '\n'), nil
}

// ErrClosed means the socket closed cleanly while the caller was
// reading or writing. Typically not an error to surface to the user.
type closedError struct{}

func (closedError) Error() string { return "ipc: connection closed" }

// ErrClosed is a sentinel value; use errors.Is to match.
var ErrClosed error = closedError{}
