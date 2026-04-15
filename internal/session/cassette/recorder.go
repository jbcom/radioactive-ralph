package cassette

import (
	"bufio"
	"context"
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
	started      time.Time

	// User-facing pipes:
	clientStdin  io.WriteCloser // what the client writes into
	clientStdout io.ReadCloser  // what the client reads out of

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
		cassettePath: cassettePath,
		cmd:          cmd,
		realStdin:    realStdin,
		realStdout:   realStdout,
		clientStdin:  clientStdinW,
		clientStdout: clientStdoutR,
		args:         append([]string(nil), args...),
	}

	// Goroutine 1: client writes → tee to cassette + forward to claude.
	go r.pumpStdin(clientStdinR, realStdin)
	// Goroutine 2: claude emits → tee to cassette + forward to client.
	go r.pumpStdout(realStdout, clientStdoutW)

	return r, nil
}

// Start spawns the claude subprocess. Returns any exec error.
func (r *Recorder) Start() error {
	r.started = time.Now()
	return r.cmd.Start()
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
	c := &Cassette{
		Version:    CurrentVersion,
		RecordedAt: r.started.UTC(),
		Args:       r.args,
		Frames:     r.frames,
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
			At:        time.Since(r.started),
			Line:      line,
		})
		_, _ = realSide.Write(line)
		_, _ = realSide.Write([]byte{'\n'})
	}
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
			At:        time.Since(r.started),
			Line:      line,
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
