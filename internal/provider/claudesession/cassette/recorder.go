package cassette

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// Recorder wraps a real claude subprocess and captures its I/O to a
// cassette. The public surface mirrors what session.Session needs:
// Stdin writer, Stdout reader, Start, Wait, Close.
//
// Typical flow:
//
//	rec, err := NewRecorder(ctx, cassettePath, "claude", argv)
//	stdin := rec.Stdin()
//	stdout := rec.Stdout()
//	rec.Start()   // spawns claude; I/O is passed through + taped
//	// ... drive session via stdin/stdout as usual ...
//	rec.Close()   // flushes cassette to disk
type Recorder struct {
	cassettePath string
	cmd          *exec.Cmd

	// startedMu guards started. The pump goroutines read it (via elapsed())
	// concurrently with Start() writing it, so it must be synchronized;
	// started stays anchored to subprocess Start() so frame timestamps and
	// RecordedAt reflect the real session, not construction.
	startedMu sync.Mutex
	started   time.Time

	// User-facing pipes:
	clientStdin  io.WriteCloser // what the client writes into
	clientStdout io.ReadCloser  // what the client reads out of

	// The OTHER ends of the client pipes, held so Close can shut them and
	// unblock/terminate the pump goroutines: pumpStdin blocks reading
	// clientStdinR until it is closed (goroutine leak otherwise), and a
	// consumer reading clientStdout to EOF hangs forever unless clientStdoutW
	// is closed after the subprocess dies.
	clientStdinR  io.ReadCloser
	clientStdoutW io.WriteCloser

	// Internal plumbing:
	realStdin  io.WriteCloser // pipe to claude's stdin
	realStdout io.ReadCloser  // pipe from claude's stdout

	mu     sync.Mutex
	frames []Frame
	args   []string
}

// NewRecorder returns an uninitialized Recorder. Callers must call
// Start before any I/O and Close after the session ends.
func NewRecorder(ctx context.Context, cassettePath, bin string, args []string) (*Recorder, error) {
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // test-controlled path
	realStdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	realStdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = realStdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Build the client-side pipes.
	clientStdinR, clientStdinW := io.Pipe()
	clientStdoutR, clientStdoutW := io.Pipe()

	r := &Recorder{
		cassettePath:  cassettePath,
		cmd:           cmd,
		realStdin:     realStdin,
		realStdout:    realStdout,
		clientStdin:   clientStdinW,
		clientStdout:  clientStdoutR,
		clientStdinR:  clientStdinR,
		clientStdoutW: clientStdoutW,
		args:          append([]string(nil), args...),
	}

	// Goroutine 1: client writes → tee to cassette + forward to claude.
	go r.pumpStdin(clientStdinR, realStdin)
	// Goroutine 2: claude emits → tee to cassette + forward to client.
	go r.pumpStdout(realStdout, clientStdoutW)

	return r, nil
}

// Start spawns the claude subprocess. Returns any exec error. The start
// time is stamped here (under startedMu) so frame timestamps anchor to the
// real session start, not construction.
func (r *Recorder) Start() error {
	r.startedMu.Lock()
	r.started = time.Now()
	r.startedMu.Unlock()
	return r.cmd.Start()
}

// startedAt returns the recorded start time under the lock.
func (r *Recorder) startedAt() time.Time {
	r.startedMu.Lock()
	defer r.startedMu.Unlock()
	return r.started
}

// elapsed returns the duration since Start() under the lock. If Start()
// has not run yet (started is zero), it returns 0.
func (r *Recorder) elapsed() time.Duration {
	t := r.startedAt()
	if t.IsZero() {
		return 0
	}
	return time.Since(t)
}

// Stdin returns the client-side writer. Anything written here is
// forwarded to claude and recorded to the cassette.
func (r *Recorder) Stdin() io.WriteCloser { return r.clientStdin }

// Stdout returns the client-side reader. Anything read here was
// emitted by claude (and also recorded).
func (r *Recorder) Stdout() io.ReadCloser { return r.clientStdout }

// Wait waits for the subprocess to exit.
func (r *Recorder) Wait() error { return r.cmd.Wait() }

// Close stops the subprocess, flushes the cassette to disk, and
// returns any write error.
func (r *Recorder) Close() error {
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		_ = r.cmd.Wait()
	}
	// Shut the client pipe ends so the pump goroutines terminate and no
	// consumer hangs: closing clientStdinR makes pumpStdin's scanner return
	// (goroutine would leak otherwise); closing clientStdoutW makes a
	// consumer reading clientStdout see EOF instead of blocking forever now
	// that the subprocess is dead.
	if r.clientStdinR != nil {
		_ = r.clientStdinR.Close()
	}
	if r.clientStdoutW != nil {
		_ = r.clientStdoutW.Close()
	}
	// Snapshot frames under the mutex: a pump goroutine may still be draining
	// a buffered line and calling append() concurrently with this read.
	r.mu.Lock()
	frames := append([]Frame(nil), r.frames...)
	r.mu.Unlock()
	c := &Cassette{
		Version:    CurrentVersion,
		RecordedAt: r.startedAt().UTC(),
		Args:       r.args,
		Frames:     frames,
	}
	return c.Save(r.cassettePath)
}

// pumpStdin reads client stdin line-by-line, appends each line to the
// cassette as a "dir=in" frame, then writes it to the real claude
// stdin.
func (r *Recorder) pumpStdin(clientSide io.Reader, realSide io.Writer) {
	scanner := bufio.NewScanner(clientSide)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		r.append(Frame{
			Direction: "in",
			At:        r.elapsed(),
			Line:      asRawJSON(line),
		})
		_, _ = realSide.Write(line)
		_, _ = realSide.Write([]byte{'\n'})
	}
}

// asRawJSON returns line as a json.RawMessage that is guaranteed to marshal:
// if line is already a valid JSON value it is stored verbatim; otherwise
// (a banner, warning, or blank line — real CLIs emit these) it is wrapped as
// a JSON string. Without this a single non-JSON captured line made the whole
// cassette's json.Encode fail at Close, silently discarding the recording.
func asRawJSON(line []byte) json.RawMessage {
	if json.Valid(line) {
		return append(json.RawMessage(nil), line...)
	}
	quoted, err := json.Marshal(string(line))
	if err != nil {
		// string always marshals; this is unreachable, but never store bytes
		// that would break the whole cassette.
		return json.RawMessage(`""`)
	}
	return quoted
}

// pumpStdout reads claude stdout line-by-line, appends each line to
// the cassette as a "dir=out" frame, then writes it to the client's
// stdout reader.
func (r *Recorder) pumpStdout(realSide io.Reader, clientSide io.Writer) {
	scanner := bufio.NewScanner(realSide)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		r.append(Frame{
			Direction: "out",
			At:        r.elapsed(),
			Line:      asRawJSON(line),
		})
		_, _ = clientSide.Write(line)
		_, _ = clientSide.Write([]byte{'\n'})
	}
}

// append adds a frame under the mutex so concurrent stdin/stdout
// writers don't race.
func (r *Recorder) append(f Frame) {
	r.mu.Lock()
	r.frames = append(r.frames, f)
	r.mu.Unlock()
}
