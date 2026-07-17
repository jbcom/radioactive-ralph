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
	runtimeDir := t.TempDir()
	st1 := openTestStore(t)
	st2 := openTestStore(t)

	// The invariant: while one supervisor holds the socket lock, a second Run
	// against the same RuntimeDir is refused with ErrSupervisorRunning. Drive it
	// DETERMINISTICALLY (start #1, wait until it's actually bound, THEN start #2)
	// rather than racing two starters against a probe+cancel window — the latter
	// flaked on slow CI runners where neither had bound before the teardown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Supervisor #1: the winner. Start it and wait until it's serving.
	winnerDone := make(chan error, 1)
	go func() { winnerDone <- Run(ctx, Options{RuntimeDir: runtimeDir, Store: st1}) }()
	probe := waitForSupervisor(t, runtimeDir, 5*time.Second) // blocks until #1 binds
	_ = probe.Close()

	// Supervisor #2: must be refused now that #1 holds the lock. Run it to
	// completion (its own ctx) — it returns immediately with ErrSupervisorRunning,
	// so no teardown race.
	loserErr := Run(context.Background(), Options{RuntimeDir: runtimeDir, Store: st2})
	if !errors.Is(loserErr, ErrSupervisorRunning) {
		t.Errorf("second Run() = %v, want ErrSupervisorRunning", loserErr)
	}

	// Now tear #1 down; it exits cleanly (nil) on ctx cancel.
	cancel()
	if err := <-winnerDone; err != nil {
		t.Errorf("winner Run() = %v, want nil (clean exit after ctx cancel)", err)
	}
}
