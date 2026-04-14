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

// Interrupt sends SIGINT to the subprocess, which Claude Code treats
// as an in-flight cancellation. Safe to call on a closed session.
func (s *Session) Interrupt() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Signal(interruptSignal)
}

// Close terminates the subprocess and waits for the reader to exit.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			// Give the subprocess a chance to finish its current turn
			// before killing it.
			_ = s.cmd.Process.Signal(interruptSignal)
		}
		s.wg.Wait()
		if s.cmd != nil {
			err = s.cmd.Wait()
		}
	})
	return err
}

// readLoop parses JSON lines from stdout and fans them out as Events.
func (s *Session) readLoop() {
	defer s.wg.Done()
	defer close(s.events)
	for s.stdoutScanner.Scan() {
		line := s.stdoutScanner.Bytes()
		var frame Inbound
		if err := json.Unmarshal(line, &frame); err != nil {
			s.events <- Event{Err: fmt.Errorf("decode frame: %w", err)}
			continue
		}
		// Retain the raw bytes. Copy — Scanner reuses its buffer.
		frame.Raw = append([]byte(nil), line...)
		// If this is the first init frame and we didn't pin an ID,
		// adopt whatever Claude chose.
		if frame.Type == "system" && frame.SessionID != "" && s.sessionID == "" {
			s.sessionID = frame.SessionID
		}
		s.events <- Event{Inbound: frame}
		if frame.Type == "result" {
			s.signalIdle()
		}
	}
	if err := s.stdoutScanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		s.events <- Event{Err: fmt.Errorf("scanner: %w", err)}
	}
}

// resetIdle creates a fresh idle channel so the next SendUserMessage
// starts a new wait cycle.
func (s *Session) resetIdle() {
	s.idleMu.Lock()
	s.idleCh = make(chan struct{})
	s.idleMu.Unlock()
}

// signalIdle closes the current idle channel exactly once.
func (s *Session) signalIdle() {
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	select {
	case <-s.idleCh: // already closed
	default:
		close(s.idleCh)
	}
}

// buildArgs assembles the argv for `claude -p`.
func buildArgs(opts Options) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	}
	if opts.ResumeMode {
		args = append(args, "--resume", opts.SessionID)
	} else {
		args = append(args, "--session-id", opts.SessionID)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	for _, t := range opts.AllowedTools {
		args = append(args, "--allowed-tools", t)
	}
	args = append(args, opts.ExtraArgs...)
	return args
}
