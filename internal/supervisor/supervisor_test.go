package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// shutdownWait bounds how long a test waits for Run to return after ctx
// cancel / Stop. This is a CORRECTNESS bound ("shutdown eventually
// completes"), not a performance assertion — a tight bound flaked on loaded
// Windows CI runners where the named-pipe unlink + DB CloseSession +
// PID-lock release round-trip occasionally took several seconds. Kept
// generous so only a genuinely-hung shutdown ever trips it.
const shutdownWait = 15 * time.Second

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "store.db")
	s, err := store.Open(context.Background(), store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// runSupervisorInBackground starts Run in a goroutine and returns a cancel
// func plus a channel that receives Run's returned error once it exits.
func runSupervisorInBackground(t *testing.T, opts Options) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts)
	}()
	return cancelFn, errCh
}

// waitForSupervisor polls Find until it succeeds or the deadline passes.
func waitForSupervisor(t *testing.T, runtimeDir string, timeout time.Duration) *ipc.Client {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		client, err := Find(runtimeDir)
		if err == nil {
			return client
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("supervisor never became reachable at %s within %s", runtimeDir, timeout)
	return nil
}

func TestRun_StartsAndAnswersStatus(t *testing.T) {
	runtimeDir := t.TempDir()
	st := openTestStore(t)

	cancel, done := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st})
	defer cancel()

	client := waitForSupervisor(t, runtimeDir, 2*time.Second)

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("client.Status: %v", err)
	}
	if status.PID == 0 {
		t.Errorf("status.PID = 0, want nonzero")
	}
	_ = client.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after ctx cancel: %v", err)
		}
	case <-time.After(shutdownWait):
		t.Fatal("Run did not exit within the shutdown bound of ctx cancel")
	}
}

func TestRun_StopCommandShutsDown(t *testing.T) {
	runtimeDir := t.TempDir()
	st := openTestStore(t)

	_, done := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st})

	client := waitForSupervisor(t, runtimeDir, 2*time.Second)

	if err := client.Stop(context.Background(), ipc.StopArgs{}); err != nil {
		t.Fatalf("client.Stop: %v", err)
	}
	_ = client.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after Stop: %v", err)
		}
	case <-time.After(shutdownWait):
		t.Fatal("Run did not exit within the shutdown bound of Stop")
	}
}

func TestRun_SecondRunRefuses(t *testing.T) {
	runtimeDir := t.TempDir()
	st1 := openTestStore(t)
	st2 := openTestStore(t)

	cancel1, done1 := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st1})
	defer cancel1()

	probe := waitForSupervisor(t, runtimeDir, 2*time.Second)
	_ = probe.Close()

	err := Run(context.Background(), Options{RuntimeDir: runtimeDir, Store: st2})
	if !errors.Is(err, ErrSupervisorRunning) {
		t.Fatalf("second Run() err = %v, want ErrSupervisorRunning", err)
	}

	cancel1()
	select {
	case <-done1:
	case <-time.After(shutdownWait):
		t.Fatal("first Run did not exit within the shutdown bound of ctx cancel")
	}
}

// TestRun_ConcurrentStartersOnlyOneWins exercises the same race a real
// crash-restart could produce: two Run calls racing Acquire against the
// same runtimeDir. Exactly one must succeed in binding; the other must see
// ErrSupervisorRunning, never a silent double-bind.
func TestRun_ConcurrentStartersOnlyOneWins(t *testing.T) {
	// The invariant: two Run calls racing Acquire against the same runtimeDir
	// must NEVER both bind — exactly one wins and the other sees
	// ErrSupervisorRunning. This variant keeps the genuine simultaneous-start
	// coverage but asserts only outcomes that hold regardless of who wins the
	// race, with bounded waits (no flaky probe+cancel window): whichever binds
	// first, at least one Run must refuse, and neither returns an unexpected
	// error. (TestRun_SecondRunRefuses covers the sequential lock-held path.)
	runtimeDir := t.TempDir()
	st1 := openTestStore(t)
	st2 := openTestStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan error, 2)
	go func() { results <- Run(ctx, Options{RuntimeDir: runtimeDir, Store: st1}) }()
	go func() { results <- Run(ctx, Options{RuntimeDir: runtimeDir, Store: st2}) }()

	// Wait until SOMETHING is serving (the winner bound), then cancel so the
	// winner exits; the loser has already refused (or is about to). Cancelling
	// BEFORE collecting both results means a hypothetical double-bind can't hang
	// the test — both would just return nil and the refusal assertion fails.
	probe := waitForSupervisor(t, runtimeDir, 5*time.Second)
	_ = probe.Close()
	cancel()

	// Collect both results (bounded). Accept either ordering of winner/loser — the
	// only forbidden outcomes are a double-bind (zero refusals) or an unexpected
	// error.
	var refusals, others int
	classifyRunResult(t, waitResult(t, results), &refusals, &others)
	classifyRunResult(t, waitResult(t, results), &refusals, &others)

	if refusals < 1 {
		t.Errorf("refusals = %d, want at least 1 (a double-bind must be impossible)", refusals)
	}
	if others != 0 {
		t.Errorf("unexpected non-nil/non-refusal Run results: %d", others)
	}
}

// waitResult reads one Run result with a bounded wait so a hung supervisor
// shutdown fails the test instead of hanging it.
func waitResult(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(shutdownWait):
		t.Fatal("a Run() did not return within the shutdown bound")
		return nil
	}
}

// classifyRunResult tallies a Run result: ErrSupervisorRunning → refusal, nil →
// a clean bind+exit (the winner), anything else → an unexpected error the test
// fails on.
func classifyRunResult(t *testing.T, err error, refusals, others *int) {
	t.Helper()
	switch {
	case errors.Is(err, ErrSupervisorRunning):
		*refusals++
	case err == nil:
		// clean exit (winner, after ctx cancel) — not counted, not an error.
	default:
		t.Errorf("unexpected Run() error: %v", err)
		*others++
	}
}
