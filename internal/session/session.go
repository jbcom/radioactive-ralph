package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/google/uuid"
)

// DefaultClaudeBin is the binary name the supervisor invokes when no
// override is provided. Tests override this to use a fake binary.
const DefaultClaudeBin = "claude"

// Session wraps a running `claude -p` subprocess speaking stream-json.
//
// Usage:
//
//	s, err := session.Spawn(ctx, session.Options{...})
//	for ev := range s.Events() {
//	    // handle ev.Inbound
//	}
//	s.SendUserMessage(ctx, "do X")
//	s.WaitForIdle(ctx)
//	s.Close()
//
// The session is single-goroutine-safe on Send* and Close; Events()
// may be consumed from any goroutine. Resume reuses the SessionID so
// Claude's conversation history is preserved across subprocess
// restarts.
type Session struct {
	// Options the session was spawned with. Immutable after Spawn.
	opts Options

	// SessionID is Claude's assigned session UUID (from the init frame
	// or explicitly pinned via Options.SessionID).
	sessionID string

	// cmd is the running subprocess.
	cmd *exec.Cmd

	// stdin writes JSON-line user messages to the subprocess.
	stdin io.WriteCloser

	// stdoutScanner reads JSON-line frames from the subprocess.
	stdoutScanner *bufio.Scanner

	// events is the fan-out channel of parsed inbound frames.
	events chan Event

	// wg tracks the reader goroutine so Close waits for it.
	wg sync.WaitGroup

	// closeOnce guards Close from being called twice.
	closeOnce sync.Once

	// idleCh is closed when the reader sees a result frame.
	idleCh chan struct{}

	// idleMu protects idleCh reset across turns.
	idleMu sync.Mutex
}

// Event is a parsed inbound frame plus any decoding error. Consumers
// handle Err before Inbound.
type Event struct {
	Inbound Inbound
	Err     error
}

// Options configures a Session spawn.
type Options struct {
	// ClaudeBin overrides DefaultClaudeBin. Tests use the fake-claude
	// binary path here.
	ClaudeBin string

	// WorkingDir is the cwd for the subprocess. Typically a worktree.
	WorkingDir string

	// SystemPrompt is the content for --append-system-prompt. Combined
	// variant + inventory biases go here.
	SystemPrompt string

	// Model pins the model tier — "haiku", "sonnet", or "opus". Empty
	// means "let claude choose" which in practice is sonnet.
	Model string

	// Effort pins the reasoning-effort level — "low", "medium",
	// "high", or "max" (as accepted by `claude --effort`). Empty
	// means "claude decides" which for sonnet is medium. Fixit's
	// advisor subprocess defaults to "high" so opus reasons deeply
	// during planning.
	Effort string

	// AllowedTools are passed to --allowed-tools. Supervisor builds
	// this from the variant profile's ToolAllowlist.
	AllowedTools []string

	// SessionID is the Claude session UUID to pin. Empty means
	// generate a new one (Spawn will populate this field).
	SessionID string

	// ResumeMode triggers `claude -p --resume <SessionID>` instead of a
	// fresh spawn. Requires a non-empty SessionID.
	ResumeMode bool

	// SentinelTaskID is the task identifier the supervisor prompts
	// about after a resume, to verify continuity. Empty in fresh spawns.
	SentinelTaskID string

	// ExtraArgs are additional `claude -p` flags the caller wants.
	ExtraArgs []string
}

// Spawn launches a new Claude subprocess and starts the reader goroutine.
//
// For ResumeMode=false, a fresh SessionID is generated (UUID v4) and
// passed via --session-id so Ralph can later resume by that ID. For
// ResumeMode=true, the SessionID must be set and is passed via
// --resume; on first successful read, Spawn emits a sentinel user
// message naming SentinelTaskID so the caller can verify continuity.
func Spawn(ctx context.Context, opts Options) (*Session, error) {
	if opts.ClaudeBin == "" {
		opts.ClaudeBin = DefaultClaudeBin
	}
	if opts.ResumeMode && opts.SessionID == "" {
		return nil, errors.New("session: ResumeMode requires SessionID")
	}
	if !opts.ResumeMode && opts.SessionID == "" {
		opts.SessionID = uuid.NewString()
	}

	args := buildArgs(opts)
	cmd := exec.CommandContext(ctx, opts.ClaudeBin, args...) //nolint:gosec // args are supervisor-controlled
	cmd.Dir = opts.WorkingDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("session: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("session: stdout pipe: %w", err)
	}
	// Stderr inherits the parent — the supervisor's log already
	// captures everything under its multiplexer pane.

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("session: start %s: %w", opts.ClaudeBin, err)
	}

	scanner := bufio.NewScanner(stdout)
	// stream-json lines can exceed the default 64KB — lift to 1MB.
	const maxLine = 1 << 20
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)

	s := &Session{
		opts:          opts,
		sessionID:     opts.SessionID,
		cmd:           cmd,
		stdin:         stdin,
		stdoutScanner: scanner,
		events:        make(chan Event, 64),
		idleCh:        make(chan struct{}),
	}
	s.wg.Add(1)
	go s.readLoop()

	if opts.ResumeMode && opts.SentinelTaskID != "" {
		sentinel := fmt.Sprintf(
			"SENTINEL: resuming task %s. Reply with the exact string `SENTINEL-OK %s` and nothing else if you remember this task.",
			opts.SentinelTaskID, opts.SentinelTaskID,
		)
		if err := s.SendUserMessage(ctx, sentinel); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("session: resume sentinel: %w", err)
		}
	}

	return s, nil
}

// SessionID returns Claude's session UUID for this process.
func (s *Session) SessionID() string { return s.sessionID }

// Events returns a channel of inbound frames. Closed when the reader
// goroutine exits (EOF or error).
func (s *Session) Events() <-chan Event { return s.events }

// SendUserMessage writes a user-role message as a JSON line on stdin.
// Resets the idle signal so a subsequent WaitForIdle waits for the
// next result frame.
func (s *Session) SendUserMessage(_ context.Context, text string) error {
	s.resetIdle()
	msg := NewUserMessage(text)
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal user msg: %w", err)
	}
	b = append(b, '\n')
	if _, err := s.stdin.Write(b); err != nil {
		return fmt.Errorf("write stdin: %w", err)
	}
	return nil
}

// WaitForIdle blocks until the subprocess emits a `type=result` frame
// or the context is cancelled.
func (s *Session) WaitForIdle(ctx context.Context) error {
	s.idleMu.Lock()
	ch := s.idleCh
	s.idleMu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
