package ipc

import (
	"testing"
	"time"
)

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
	srv, _ := newTestServer(t, &fakeHandler{})

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
	srv, _ := newTestServer(t, &fakeHandler{})
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
