package ipc

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

// transientOnceListener returns one transient (non-ErrClosed) Accept error,
// then blocks until Close, at which point it returns net.ErrClosed. It lets
// a test assert acceptLoop keeps running past a transient error and only
// exits on the fatal closed-listener case.
type transientOnceListener struct {
	mu        sync.Mutex
	yielded   bool
	closed    chan struct{}
	closeOnce sync.Once
	accepts   int // total Accept() calls observed
}

func newTransientOnceListener() *transientOnceListener {
	return &transientOnceListener{closed: make(chan struct{})}
}

func (l *transientOnceListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	l.accepts++
	first := !l.yielded
	l.yielded = true
	l.mu.Unlock()

	if first {
		// A transient error the loop MUST survive (mimics EMFILE).
		return nil, &net.OpError{Op: "accept", Err: errors.New("too many open files")}
	}
	// After the transient error, block until Close, then report the fatal
	// closed-listener error so the loop exits.
	<-l.closed
	return nil, net.ErrClosed
}

func (l *transientOnceListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *transientOnceListener) Addr() net.Addr { return dummyAddr{} }

func (l *transientOnceListener) acceptCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.accepts
}

type dummyAddr struct{}

func (dummyAddr) Network() string { return "test" }
func (dummyAddr) String() string  { return "test" }

// TestAcceptLoopSurvivesTransientError is the regression for the audit's
// high-severity finding: a transient (non-ErrClosed) Accept error must NOT
// terminate the accept goroutine — otherwise one fd-pressure blip
// permanently wedges the whole control plane. The loop must log-and-continue
// on the transient error and only exit when the listener is closed.
func TestAcceptLoopSurvivesTransientError(t *testing.T) {
	l := newTransientOnceListener()
	s := &Server{
		listener: l,
		stopCh:   make(chan struct{}),
		// Production always sets a non-nil logger (see NewServer); acceptLoop
		// logs the transient error, so a nil logger would panic.
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	s.wg.Add(1)

	done := make(chan struct{})
	go func() {
		s.acceptLoop()
		close(done)
	}()

	// Wait until the loop has made it PAST the transient error into its
	// second Accept (which blocks on Close). If the bug were present, the
	// goroutine would have returned after the first Accept and `done` would
	// close before we ever reach a second Accept.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if l.acceptCount() >= 2 {
			break
		}
		select {
		case <-done:
			t.Fatal("acceptLoop exited after the transient error; it must continue")
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if l.acceptCount() < 2 {
		t.Fatalf("acceptLoop made only %d Accept call(s); expected it to retry past the transient error", l.acceptCount())
	}

	// Now close the listener — the loop must exit on the fatal ErrClosed.
	_ = l.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("acceptLoop did not exit after the listener was closed")
	}
}

// ensure the fake satisfies net.Listener and net.Conn plumbing compiles.
var _ net.Listener = (*transientOnceListener)(nil)
var _ io.Closer = (*transientOnceListener)(nil)

