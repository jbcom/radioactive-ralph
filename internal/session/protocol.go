// Package session wraps a `claude -p` subprocess so the supervisor can
// drive it via stream-json over stdin/stdout.
//
// The stream-json protocol pairs message objects, one per JSON line:
//
//   - Messages FROM the supervisor to Claude carry `{"type":"user", …}`
//     with a user-scoped content block.
//   - Messages FROM Claude carry `{"type":"assistant", …}` or
//     `{"type":"result", …}` when the agent finishes its turn.
//
// Only the shape Ralph depends on is modeled here. Unknown fields are
// preserved as raw JSON so a future Claude Code version that adds a
// field doesn't break replay.
package session

import (
	"encoding/json"
)

// Inbound is a JSON-line frame received from `claude -p`. Each frame is
// one of several shapes depending on type; the raw bytes are retained
// so downstream consumers (event log, supervisor) can re-emit or
// re-parse as needed.
type Inbound struct {
	// Type is the top-level message kind. Common values: "assistant",
	// "user", "result", "system". Unknown values are preserved as
	// strings and handled gracefully.
	Type string `json:"type"`

	// SessionID is Claude's view of its session UUID. The supervisor
	// uses this to correlate resume operations.
	SessionID string `json:"session_id,omitempty"`

	// Subtype narrows the meaning of Type. For type=result, the
	// subtypes Ralph cares about are "success" (agent done, clean exit)
	// and "error_max_turns" (turn cap hit).
	Subtype string `json:"subtype,omitempty"`

	// Message is the assistant/user payload for type in {assistant, user}.
	// Kept as a raw JSON so we don't flatten Claude's content-block shape.
	Message json.RawMessage `json:"message,omitempty"`

	// Result is the final result payload for type=result.
	Result json.RawMessage `json:"result,omitempty"`

	// Raw is the full JSON line as received, retained for event-log
	// archival.
	Raw []byte `json:"-"`
}

// Outbound is a JSON-line frame sent TO `claude -p`. Only user messages
// and interrupts are supported — the CLI does not accept assistant or
// system messages over stdin.
type Outbound struct {
	Type    string        `json:"type"`
	Message OutboundInner `json:"message"`
}

// OutboundInner mirrors the subset of the Anthropic message shape that
// `claude -p --input-format stream-json` accepts.
type OutboundInner struct {
	Role    string                `json:"role"`
	Content []OutboundContentPart `json:"content"`
}

// OutboundContentPart is a single content block. Ralph only emits text.
type OutboundContentPart struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// NewUserMessage builds an Outbound frame wrapping the given text as a
// single user-role text block.
func NewUserMessage(text string) Outbound {
	return Outbound{
		Type: "user",
		Message: OutboundInner{
			Role: "user",
			Content: []OutboundContentPart{
				{Type: "text", Text: text},
			},
		},
	}
}
