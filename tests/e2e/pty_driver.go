package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// Keystroke bytes for driving a real terminal client, per the E2E
// strategy notes (directive-additions.md "E2E testing tooling"):
// arrows/enter/esc are ANSI CSI sequences, not the tea.KeyMsg values
// Layer 1 sends directly — this IS the "like an actual user" test, so it
// writes the literal bytes a real keyboard would produce.
var (
	KeyDown  = []byte("\x1b[B")
	KeyUp    = []byte("\x1b[A")
	KeyRight = []byte("\x1b[C")
	KeyLeft  = []byte("\x1b[D")
	KeyEnter = []byte("\r")
	// KeyEsc is a bare ESC byte. Bubble Tea's terminal reader must
	// briefly wait to see whether a lone 0x1b is an Esc keypress or the
	// first byte of a CSI sequence (arrow keys, etc.) before delivering
	// it as tea.KeyEsc, so a test sending this immediately after another
	// keystroke can race that disambiguation window. Prefer KeyLeft (the
	// model's handleKey treats esc/backspace/left/h as equivalent
	// drill-out keys) wherever the test doesn't specifically need to
	// exercise the Esc key itself — it carries no such ambiguity.
	KeyEsc  = []byte("\x1b")
	KeyQuit = []byte("q")
)

// PTYProcess is a process spawned under a pty, with an expect/send driver
// bolted on. The TUI's content lives in the terminal's alternate screen
// buffer (Bubble Tea's alt-screen init), so Expect scans the RAW byte
// stream rather than trying to track cursor/screen state — a substring
// search over everything read so far is sufficient and far simpler than
// a full terminal emulator, and matches the "assert on rendered output"
// approach Layer 1 already uses via teatest.
type PTYProcess struct {
	t    *testing.T
	cmd  *exec.Cmd
	ptmx *os.File

	mu  sync.Mutex
	buf bytes.Buffer

	readErr  error
	readDone chan struct{}
}

// StartPTY spawns binPath with args under env in a real pty and begins
// draining ptmx into an internal buffer immediately so no output is ever
// lost to an unread fd between spawn and the first Expect call.
func StartPTY(t *testing.T, binPath string, env *IsolatedEnv, args ...string) *PTYProcess {
	t.Helper()
	cmd := exec.Command(binPath, args...) //nolint:gosec // binPath is our own just-built binary
	cmd.Dir = env.ProjectDir
	cmd.Env = env.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start(%s %v): %v", binPath, args, err)
	}

	p := &PTYProcess{
		t:        t,
		cmd:      cmd,
		ptmx:     ptmx,
		readDone: make(chan struct{}),
	}
	go p.drain()
	return p
}

func (p *PTYProcess) drain() {
	defer close(p.readDone)
	buf := make([]byte, 4096)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.buf.Write(buf[:n])
			p.mu.Unlock()
		}
		if err != nil {
			p.mu.Lock()
			p.readErr = err
			p.mu.Unlock()
			return
		}
	}
}

// snapshot returns everything read from the pty so far.
func (p *PTYProcess) snapshot() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]byte, p.buf.Len())
	copy(out, p.buf.Bytes())
	return out
}

// Expect polls the accumulated pty output until substr appears or timeout
// elapses, failing the test with the full captured output on timeout so
// a failure is immediately diagnosable rather than a bare "timed out".
func (p *PTYProcess) Expect(substr string, timeout time.Duration) {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if bytes.Contains(p.snapshot(), []byte(substr)) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.t.Fatalf("PTYProcess.Expect(%q) timed out after %s; captured output:\n%s", substr, timeout, p.snapshot())
}

// ExpectFunc polls until cond(capturedOutput) is true or timeout elapses.
// Useful when the assertion needs more than a single substring (e.g. two
// substrings both present).
func (p *PTYProcess) ExpectFunc(desc string, timeout time.Duration, cond func([]byte) bool) {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond(p.snapshot()) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.t.Fatalf("PTYProcess.ExpectFunc(%s) timed out after %s; captured output:\n%s", desc, timeout, p.snapshot())
}

// Send writes bytes to the pty's controlling side, exactly as a
// keystroke would arrive on the client's stdin.
func (p *PTYProcess) Send(b []byte) {
	p.t.Helper()
	if _, err := p.ptmx.Write(b); err != nil {
		p.t.Fatalf("PTYProcess.Send(%q): %v", b, err)
	}
}

// Wait blocks until the underlying process exits, returning its error
// (nil on a clean exit(0)). Bounded by timeout so a client that fails to
// quit cleanly fails the test instead of hanging the whole suite.
func (p *PTYProcess) Wait(timeout time.Duration) error {
	p.t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = p.cmd.Process.Kill()
		p.t.Fatalf("PTYProcess.Wait: process did not exit within %s; captured output:\n%s", timeout, p.snapshot())
		return fmt.Errorf("unreachable")
	}
}

// Close releases the pty file descriptor. Safe to call after Wait.
func (p *PTYProcess) Close() {
	_ = p.ptmx.Close()
}
