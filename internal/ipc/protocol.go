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
// drive commands (plan-import/plan-set-status/task-approve/worker-kill) are v2;
// blocked-task recovery and hierarchical team telemetry are v3.
// A client omitting Request.ProtoVersion is treated as v1 for back-compat.
const ProtoVersion = 3

// Command names for the JSON-line protocol.
const (
	// v1 — observe surface.
	CmdStatus       = "status"
	CmdAttach       = "attach"
	CmdEnqueue      = "enqueue"
	CmdStop         = "stop"
	CmdReloadConfig = "reload-config"

	// v2 — drive surface (see the IPC drive-api design spec).
	CmdPlanImport      = "plan-import"
	CmdPlanSetStatus   = "plan-set-status"
	CmdTaskApprove     = "task-approve"
	CmdTaskRetry       = "task-retry"
	CmdTaskList        = "task-list"
	CmdWorkerKill      = "worker-kill"
	CmdCalibrationPut  = "calibration-put"
	CmdCalibrationGet  = "calibration-get"
	CmdCalibrationList = "calibration-list"
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
	Teams         []TeamSummary   `json:"teams,omitempty"`
	LastEventAt   time.Time       `json:"last_event_at,omitempty"`
	HeartbeatAge  time.Duration   `json:"heartbeat_age_ns,omitempty"`
}

// WorkerSummary is the runtime-facing status for one in-flight worker.
type WorkerSummary struct {
	// WorkerID is the store worker-row id — the value a client passes to the
	// worker-kill drive command to target THIS worker. Distinct from any
	// provider-session id.
	WorkerID           string `json:"worker_id"`
	PlanID             string `json:"plan_id"`
	TaskID             string `json:"task_id"`
	Provider           string `json:"provider,omitempty"`
	Alias              string `json:"alias,omitempty"`
	TeamPath           string `json:"team_path,omitempty"`
	Model              string `json:"model,omitempty"`
	Effort             string `json:"effort,omitempty"`
	IndependenceDomain string `json:"independence_domain,omitempty"`
	AssignedSessionID  string `json:"assigned_session_id,omitempty"`
	ProviderSessionID  string `json:"provider_session_id,omitempty"`
}

// TeamSummary is one hierarchical team-prefix rollup. A task assigned to
// studio/narrative contributes to both studio and studio/narrative.
type TeamSummary struct {
	TeamPath      string         `json:"team_path"`
	Total         int            `json:"total"`
	Pending       int            `json:"pending"`
	Ready         int            `json:"ready"`
	Running       int            `json:"running"`
	Done          int            `json:"done"`
	Blocked       int            `json:"blocked"`
	Failed        int            `json:"failed"`
	ActiveWorkers int            `json:"active_workers"`
	Providers     map[string]int `json:"providers,omitempty"`
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

// AttachArgs is the client's payload when opening an event stream via
// CmdAttach. ProjectID scopes the stream — the IPC connection carries no
// implicit project (the supervisor serves every project on one socket), so the
// client names it, as the drive commands do. AfterID is the client-owned resume
// cursor: the stream carries every event with id strictly greater than AfterID.
// AfterID=0 means "from the beginning" — the CLIENT, not the server, picks the
// live-tail cursor by first reading MaxEventID (or the backlog's max id) and
// passing it here. A reconnecting client passes the highest id it has processed,
// resuming with no gap and no duplicate.
type AttachArgs struct {
	ProjectID string `json:"project_id"`
	AfterID   int64  `json:"after_id,omitempty"`
}

// AttachEvent is one event streamed over an Attach connection: the public,
// versioned shape of an events-table row. It is deliberately NOT the raw store
// row — Payload is the kind-specific JSON passed through verbatim, so adding a
// new event kind never requires a transport change. ID lets a client persist
// its resume cursor for reconnects.
type AttachEvent struct {
	ID         int64           `json:"id"`
	Kind       string          `json:"kind"`
	Stream     string          `json:"stream,omitempty"`
	PlanID     string          `json:"plan_id,omitempty"`
	TaskID     string          `json:"task_id,omitempty"`
	Actor      string          `json:"actor,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
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

// TaskRetryArgs requeues a task blocked by stale inputs or an unavailable
// calibrated capability after the operator has corrected the condition.
type TaskRetryArgs struct {
	PlanID string `json:"plan_id"`
	TaskID string `json:"task_id"`
}

// TaskListArgs scopes the v3 task/provenance read API to one plan.
type TaskListArgs struct {
	PlanID string `json:"plan_id"`
}

// TaskSummary exposes the complete operator-relevant v2 scheduling and
// completion provenance without leaking the store's SQL row shape.
type TaskSummary struct {
	PlanID                     string `json:"plan_id"`
	TaskID                     string `json:"task_id"`
	Description                string `json:"description"`
	Status                     string `json:"status"`
	TeamPath                   string `json:"team_path,omitempty"`
	AssignedAlias              string `json:"assigned_alias,omitempty"`
	AssignedProvider           string `json:"assigned_provider,omitempty"`
	AssignedModel              string `json:"assigned_model,omitempty"`
	AssignedEffort             string `json:"assigned_effort,omitempty"`
	AssignedIndependenceDomain string `json:"assigned_independence_domain,omitempty"`
	AssignedSessionID          string `json:"assigned_session_id,omitempty"`
	ProviderSessionID          string `json:"provider_session_id,omitempty"`
	CalibrationID              string `json:"calibration_id,omitempty"`
	CapabilitySetJSON          string `json:"capability_set_json,omitempty"`
	BlockedReason              string `json:"blocked_reason,omitempty"`
	CompletionEvidenceJSON     string `json:"completion_evidence_json,omitempty"`
}

// TaskListReply contains complete task summaries for one plan.
type TaskListReply struct {
	Tasks []TaskSummary `json:"tasks"`
}

// CalibrationRecord is the wire representation of one immutable calibrated
// execution lane.
type CalibrationRecord struct {
	ID                 string          `json:"id,omitempty"`
	Alias              string          `json:"alias"`
	Provider           string          `json:"provider"`
	Model              string          `json:"model"`
	Effort             string          `json:"effort"`
	BinaryPath         string          `json:"binary_path"`
	BinaryVersion      string          `json:"binary_version"`
	BinarySHA256       string          `json:"binary_sha256"`
	InvocationHash     string          `json:"invocation_hash"`
	InferenceDomain    string          `json:"inference_domain"`
	ControlDomain      string          `json:"control_domain"`
	IndependenceDomain string          `json:"independence_domain"`
	ModelDigest        string          `json:"model_digest,omitempty"`
	Capabilities       []string        `json:"capabilities"`
	Evidence           json.RawMessage `json:"evidence"`
}

// CalibrationPutArgs carries one calibration record to authenticated ingress.
type CalibrationPutArgs struct {
	Calibration CalibrationRecord `json:"calibration"`
}

// CalibrationPutReply reports the immutable content address assigned on import.
type CalibrationPutReply struct {
	ID string `json:"id"`
}

// CalibrationGetArgs identifies one calibration by content address.
type CalibrationGetArgs struct {
	ID string `json:"id"`
}

// CalibrationListReply contains immutable calibration records in alias order.
type CalibrationListReply struct {
	Calibrations []CalibrationRecord `json:"calibrations"`
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
