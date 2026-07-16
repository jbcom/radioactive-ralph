// Package a2a is a thin adoption layer over github.com/a2aproject/a2a-go/v2's
// core vocabulary (github.com/a2aproject/a2a-go/v2/a2a), per
// .agent-state/decisions.ndjson ("a2a-comms-layer") and spec §12.
//
// We import ONLY the a2a-go core-types package (a2a/), which is stdlib-only
// (plus github.com/google/uuid) — never a2asrv/a2agrpc/a2apb, which pull in
// grpc/protobuf. This package re-exports the TaskState constants radioactive
// -ralph actually uses, and defines Evidence: the structured result a worker
// submits to the orchestrator, carried as an a2a.Message so worker<->
// orchestrator communication uses the standard vocabulary.
//
// The a2a_messages store table (see internal/store/schema/0002_a2a.up.sql)
// is a message/evidence LOG, not a parallel task store — the store's
// existing tasks table is the durable DAG (§ decision "a2a-comms-layer":
// "Don't create a parallel task store — the plan DAG IS the task store").
//
// Critically: nothing in this package marks a task done. A worker
// submitting Evidence via an a2a.Message is a proposal for the
// orchestrator to verify (see internal/orch.VerifyAndComplete) — never a
// self-assertion of completion.
package a2a

import (
	"encoding/json"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// TaskState re-exports a2a.TaskState so callers need not import the
// upstream package directly for the states radioactive-ralph drives.
type TaskState = a2a.TaskState

// The TaskState values radioactive-ralph uses to describe a worker's
// progress against one store task. The orchestrator drives these
// transitions; a worker only ever SUGGESTS one via submitted Evidence.
const (
	// StateSubmitted: the task has been claimed and dispatched to a worker
	// but the worker has not yet reported evidence.
	StateSubmitted = a2a.TaskStateSubmitted
	// StateWorking: the worker is actively executing.
	StateWorking = a2a.TaskStateWorking
	// StateInputRequired is the never-block signal: a worker that would
	// otherwise block waiting for input is in this TASK STATE, which the
	// orchestrator handles by re-dispatching with an answer or by
	// kill+reclaim — never by waiting on a blocked pty.
	StateInputRequired = a2a.TaskStateInputRequired
	// StateCompleted: ORCHESTRATOR-VERIFIED completion only. Never set
	// directly from a worker's self-report or from process termination.
	StateCompleted = a2a.TaskStateCompleted
	// StateFailed: verification rejected the evidence, or the worker
	// reported failure.
	StateFailed = a2a.TaskStateFailed
)

// Message and Part re-export the a2a-go core types used to carry Evidence.
type (
	Message = a2a.Message
	Part    = a2a.Part
)

// Role constants used when constructing evidence messages.
const (
	RoleAgent = a2a.MessageRoleAgent
	RoleUser  = a2a.MessageRoleUser
)

// Evidence is what a worker submits after it believes it has completed a
// task: what it ran, what happened, and what changed. This is a PROPOSAL —
// the orchestrator's VerifyAndComplete re-checks it against the task's
// acceptance criteria before ever marking the task done in the store.
type Evidence struct {
	// Ran is the mechanical check the worker believes satisfies the task's
	// acceptance criterion (e.g. "go test ./..." or a file path). Advisory
	// only — the orchestrator re-runs the real check itself rather than
	// trusting this string.
	Ran string `json:"ran,omitempty"`

	// ExitCode is the exit status the worker observed when it ran Ran, if
	// applicable. Advisory only, same caveat as Ran.
	ExitCode int `json:"exit_code"`

	// Output is a bounded capture of what Ran produced (stdout/stderr),
	// kept for the audit trail and for judgment-based acceptance criteria.
	Output string `json:"output,omitempty"`

	// Diff is the worker's summary/patch of what it changed, if any.
	Diff string `json:"diff,omitempty"`

	// FilesChanged lists paths the worker believes it modified.
	FilesChanged []string `json:"files_changed,omitempty"`
}

// NewEvidenceMessage wraps Evidence as an a2a.Message with a single JSON
// data Part, tagged with the given plan/task context. role is typically
// RoleAgent (a worker reporting evidence to the orchestrator).
func NewEvidenceMessage(role a2a.MessageRole, taskID, contextID string, ev Evidence) *Message {
	msg := a2a.NewMessage(role, a2a.NewDataPart(ev))
	msg.TaskID = a2a.TaskID(taskID)
	msg.ContextID = contextID
	return msg
}

// MarshalEvidence serializes Evidence to a JSON string for storage in the
// store's evidence/payload columns (e.g. MarkDone's evidenceJSON
// parameter).
func MarshalEvidence(ev Evidence) (string, error) {
	raw, err := json.Marshal(ev)
	if err != nil {
		return "", fmt.Errorf("a2a: marshal evidence: %w", err)
	}
	return string(raw), nil
}

// UnmarshalEvidence parses a JSON string back into Evidence. Used when
// reloading a worker's submitted evidence (e.g. from an a2a_messages row
// or a stored event payload) for (re-)verification.
func UnmarshalEvidence(raw string) (Evidence, error) {
	var ev Evidence
	if raw == "" {
		return ev, nil
	}
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		return Evidence{}, fmt.Errorf("a2a: unmarshal evidence: %w", err)
	}
	return ev, nil
}

// EvidenceFromMessage extracts Evidence from an a2a.Message's first Data
// part. Returns an error if the message carries no data part.
func EvidenceFromMessage(msg *Message) (Evidence, error) {
	if msg == nil {
		return Evidence{}, fmt.Errorf("a2a: nil message")
	}
	for _, p := range msg.Parts {
		if p == nil {
			continue
		}
		if data := p.Data(); data != nil {
			// Round-trip through JSON since Data() returns `any` (it was
			// stored as a concrete Evidence at construction time in this
			// process, but a message reloaded from JSON storage carries
			// map[string]any instead).
			raw, err := json.Marshal(data)
			if err != nil {
				return Evidence{}, fmt.Errorf("a2a: remarshal data part: %w", err)
			}
			var ev Evidence
			if err := json.Unmarshal(raw, &ev); err != nil {
				return Evidence{}, fmt.Errorf("a2a: decode evidence data part: %w", err)
			}
			return ev, nil
		}
	}
	return Evidence{}, fmt.Errorf("a2a: message has no data part")
}
