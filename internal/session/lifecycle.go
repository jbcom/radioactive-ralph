package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

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
	// `--output-format=stream-json` requires `--verbose` per the
	// Claude Code CLI (as of 2.1.x). Omitting it yields:
	//   "Error: When using --print, --output-format=stream-json
	//    requires --verbose"
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
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
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}
	for _, t := range opts.AllowedTools {
		args = append(args, "--allowed-tools", t)
	}
	args = append(args, opts.ExtraArgs...)
	return args
}
