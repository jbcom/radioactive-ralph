package ipc

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// startStopRaceServer builds and starts a server for these tests.
//
// Deliberately NOT reusing the helper in server_safety_test.go: that file is
// //go:build !windows, and the deadlock these tests exist for is
// Windows-only — a helper unavailable on the one platform that can fail is no
// helper at all.
func startStopRaceServer(t *testing.T) *Server {
	t.Helper()
	dir := shortTempDir(t)
	socketPath, heartbeatPath := ServiceEndpoint(dir)
	srv, err := NewServer(ServerOptions{
		SocketPath:        socketPath,
		HeartbeatPath:     heartbeatPath,
		HeartbeatInterval: 20 * time.Millisecond,
		Handler:           &fakeHandler{},
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return srv
}

// TestStopReturnsPromptlyWithAnAcceptInFlight pins the shutdown contract that a
// CI hang exposed: Stop must return quickly when the accept loop is parked in
// Accept() with no client connecting, which is the state EVERY idle server is
// in.
//
// The Windows named-pipe listener makes this sharp. winio's Accept() hands a
// response channel to an internal listener goroutine and then blocks on it with
// no cancellation case, while that goroutine selects between "a close was
// requested" and "an accept was requested". With both ready the select picks at
// random, so a close can lose the coin flip; the goroutine then calls
// ConnectNamedPipe for a client that will never arrive, and Close's wait for it
// to exit never returns. Observed as a 10-minute test timeout in
// TestAcquire_SecondFailsWhileFirstHolds, with Stop parked in
// win32PipeListener.Close.
//
// The fix is ordering: signal the accept loop and let it leave Accept() before
// closing the listener, so no accept request is ever in flight during close.
// Asserting a bound rather than "no deadlock" is what makes the race
// detectable — a deadlocked Stop fails this in seconds instead of hanging the
// whole package for ten minutes.
func TestStopReturnsPromptlyWithAnAcceptInFlight(t *testing.T) {
	srv := startStopRaceServer(t)

	// Let the accept loop actually reach Accept() — the race only exists once a
	// request is queued with the listener goroutine.
	time.Sleep(50 * time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return within 10s with an accept in flight — the accept loop must leave Accept() before the listener is closed")
	}
}

// TestStopIsIdempotentAfterAcceptShutdown guards the ordering change itself: a
// second Stop must still be a clean no-op, not a double-close panic or a wait
// on a goroutine that has already exited.
func TestStopIsIdempotentAfterAcceptShutdown(t *testing.T) {
	srv := startStopRaceServer(t)
	time.Sleep(50 * time.Millisecond)

	if err := srv.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second Stop hung; it must be a no-op")
	}
}

// TestStopReturnsEvenWhenTheAcceptLoopWillNotRetire is the follow-up finding:
// bounding the WAIT is not the same as bounding Stop.
//
// The first version timed out waiting for the accept loop and then fell
// through to the synchronous listener.Close() — the very call documented as
// able to block forever in exactly that state. So a failed wake-up dial or a
// slow scheduler still wedged shutdown, with the timeout providing only the
// appearance of a bound.
//
// Stop must return within its bound whether or not retirement succeeds. When
// it does not, closing the listener moves to a detached goroutine: leaking one
// blocked goroutine in a process that is shutting down anyway is strictly
// better than never shutting down.
func TestStopReturnsEvenWhenTheAcceptLoopWillNotRetire(t *testing.T) {
	srv := startStopRaceServer(t)
	time.Sleep(50 * time.Millisecond)

	// Reproduce the Windows failure portably. On Unix, closing a listener out
	// from under a parked Accept() returns immediately, so the bug cannot occur
	// here naturally — substituting a listener whose Close blocks forever is
	// what makes this test meaningful on every platform rather than only on the
	// one that can actually deadlock.
	srv.listener = blockingCloseListener{Listener: srv.listener}
	srv.acceptDone = make(chan struct{})

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- srv.Stop() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
		// Generous, but far below "forever" — the point is that Stop returns at
		// all when retirement cannot complete.
		if elapsed := time.Since(start); elapsed > 3*acceptRetireTimeout {
			t.Fatalf("Stop took %s; it must return on its own bound rather than "+
				"waiting on a close that may never finish", elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Stop never returned when the accept loop could not be retired — " +
			"the timeout only bounded the WAIT, not Stop itself")
	}
}

// blockingCloseListener stands in for winio's named-pipe listener in the state
// where its Close never returns.
type blockingCloseListener struct {
	net.Listener
}

func (blockingCloseListener) Close() error {
	select {} // never returns, exactly like the deadlocked win32PipeListener.Close
}
