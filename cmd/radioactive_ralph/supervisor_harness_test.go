package main

import (
	"context"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

// startTestSupervisor runs a real supervisor against stateDir and waits until it
// is reachable, returning its store.
//
// Extracted because every CLI command is now a dumb client that REFUSES to run
// without a supervisor — so a test exercising any of them needs one, and the
// spin-up-and-wait dance was already copy-pasted three times before this.
// Duplicating it a fourth time would mean four places to fix when the readiness
// signal changes.
func startTestSupervisor(t *testing.T, stateDir string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		// The error is intentionally dropped: cancelling the context on cleanup
		// is the normal shutdown path, and reporting that as a failure would
		// make every test using this helper fail on teardown.
		_ = supervisor.Run(ctx, supervisor.Options{RuntimeDir: stateDir, Store: st})
	}()

	// Poll rather than sleep a fixed interval: the supervisor is ready when its
	// socket answers, and a fixed sleep is either too short on a loaded machine
	// or wasted time on a fast one.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, ferr := supervisor.Find(stateDir); ferr == nil {
			_ = c.Close()
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("supervisor did not become reachable at %s", stateDir)
	return nil
}
