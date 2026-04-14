// Package ipc is radioactive-ralph's supervisor-client IPC layer.
//
// The supervisor listens on a Unix domain socket under its workspace's
// sessions/ directory. `ralph status`, `ralph attach`, `ralph enqueue`,
// `ralph stop`, and `ralph reload-config` connect to the same socket
// and exchange newline-delimited JSON messages.
//
// Heartbeat liveness is signalled via the supervisor touching an
// `.alive` file every few seconds. `ralph status` checks the file's
// mtime before even attempting a socket connect — if the supervisor
// crashed and left a stale socket, we want to surface the dead-daemon
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

// Command names for the JSON-line protocol.
const (
	CmdStatus       = "status"
	CmdAttach       = "attach"
	CmdEnqueue      = "enqueue"
	CmdStop         = "stop"
	CmdReloadConfig = "reload-config"
)

// Request is a single command from a client to the supervisor.
type Request struct {
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is the single-shot reply shape. For streaming commands the
// server sends multiple Event frames followed by a final Response with
// Ok=true; mid-stream errors send a Response with Ok=false.
type Response struct {
	Ok    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// StreamEvent is one frame emitted during a streaming command (e.g. attach).
type StreamEvent struct {
	Event json.RawMessage `json:"event"`
}

// StatusReply is the data payload for CmdStatus responses. Keeping this
// a plain struct rather than map[string]any makes versioning/migration
// explicit — callers know at compile-time what fields to expect.
type StatusReply struct {
	Variant        string        `json:"variant"`
	PID            int           `json:"pid"`
	Uptime         time.Duration `json:"uptime_ns"`
	ActiveSessions int           `json:"active_sessions"`
	QueuedTasks    int           `json:"queued_tasks"`
	RunningTasks   int           `json:"running_tasks"`
	LastEventAt    time.Time     `json:"last_event_at,omitempty"`
	HeartbeatAge   time.Duration `json:"heartbeat_age_ns,omitempty"`
}

// EnqueueArgs is the client's payload when pushing work via CmdEnqueue.
type EnqueueArgs struct {
	TaskID      string `json:"task_id"` // optional; supervisor generates UUID if empty
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
