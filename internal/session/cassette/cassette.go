// Package cassette provides a VCR-style record/replay layer for the
// session wrapper. Cassettes capture the stdin (user messages) and
// stdout (stream-json frames) of a real `claude -p` subprocess so
// tests can replay the same conversation deterministically without
// needing API credentials.
//
// The design is subprocess-level rather than HTTP-level: claude's
// actual API calls happen inside the subprocess and we never see the
// HTTP traffic. Cassettes therefore replay the observable I/O of
// claude itself, which is exactly the surface session.Session
// depends on.
//
// Cassette usage flow:
//
//  1. Record once (requires ANTHROPIC_API_KEY or authenticated
//     Claude Code install):
//
//     rec, _ := cassette.NewRecorder(cassettePath, realClaudeBin, args)
//     // rec exposes the same file descriptors Spawn uses
//     // drive the session normally, rec writes the JSON cassette
//
//  2. Replay (no auth required, runs in CI):
//
//     opts.ClaudeBin = cassette.ReplayerPath
//     os.Setenv("RALPH_CASSETTE_PATH", cassettePath)
//     // session.Spawn exec's the replayer, which reads the cassette
//     // and emits the recorded frames with recorded timing.
//
// Cassettes are JSON so diffs in code review are meaningful.
package cassette

import (
	"encoding/json"
	"os"
	"time"
)

// Cassette is the top-level on-disk format.
type Cassette struct {
	// Version identifies the cassette schema. Bumped on
	// incompatible changes.
	Version int `json:"version"`

	// RecordedAt is when the cassette was captured. Informational.
	RecordedAt time.Time `json:"recorded_at"`

	// ClaudeVersion is whatever `claude --version` reported when the
	// cassette was recorded. Tests can warn (not fail) when the
	// installed claude differs.
	ClaudeVersion string `json:"claude_version,omitempty"`

	// Args are the CLI args claude was invoked with at record time,
	// minus --session-id (which the replayer must honor from the
	// actual invocation).
	Args []string `json:"args,omitempty"`

	// Frames is the ordered stream of recorded events. Each frame is
	// either an inbound frame from claude (stdin → stdout direction
	// from the cassette's POV, which means stdout from the
	// replayer's POV) or a user-input marker noting when stdin
	// received a line from the client.
	Frames []Frame `json:"frames"`
}

// Frame is one entry in a cassette.
type Frame struct {
	// Direction is "in" (stdin received from client) or "out" (stdout
	// emitted to client). During replay, "in" frames act as
	// checkpoints: the replayer waits for the client to send a
	// matching user message before emitting subsequent "out" frames.
	Direction string `json:"dir"`

	// At is the offset from the session start when this frame was
	// observed, in nanoseconds. Replayer enforces a minimum gap
	// between consecutive "out" frames based on these offsets.
	At time.Duration `json:"at_ns"`

	// Line is the raw JSON line that was observed (no trailing \n).
	// For "in" frames, this is the outbound stream-json object the
	// client sent. For "out" frames, this is the inbound frame
	// claude emitted.
	Line json.RawMessage `json:"line"`
}

// CurrentVersion is the schema version the recorder writes.
const CurrentVersion = 1

// Save writes c to path as indented JSON for readable diffs.
func (c *Cassette) Save(path string) error {
	f, err := os.Create(path) //nolint:gosec // test-controlled path
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// Load reads a cassette from disk.
func Load(path string) (*Cassette, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		return nil, err
	}
	var c Cassette
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
