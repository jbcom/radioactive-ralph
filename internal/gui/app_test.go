//go:build gui

package gui

import (
	"context"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestRun_StartsAndStopsCleanly launches the GUI under the headless test driver
// with a fake controller, confirms it painted the initial macro view, and shuts
// down cleanly when the context is cancelled (the refresh + attach goroutines
// must observe cancellation and exit — no hang).
func TestRun_StartsAndStopsCleanly(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "Ship It", Status: store.PlanStatusActive}}
	f.status = ipc.StatusReply{ActivePlans: 1, ReadyTasks: 2}

	ctx, cancel := context.WithCancel(context.Background())
	a := test.NewApp()
	t.Cleanup(a.Quit)

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, Opts{Controller: f, ProjectID: "proj", fyneApp: a})
	}()

	// Give Run a moment to build the window and paint.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(a.Driver().AllWindows()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(a.Driver().AllWindows()) == 0 {
		t.Fatal("GUI window never opened")
	}

	// Cancel and confirm Run returns (its goroutines exit on ctx.Done).
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s of context cancel")
	}
}

// TestRun_RequiresController confirms Run rejects a nil controller rather than
// panicking on first use.
func TestRun_RequiresController(t *testing.T) {
	if err := Run(context.Background(), Opts{}); err == nil {
		t.Error("Run with nil Controller: want error, got nil")
	}
}

// TestEventTriggersRefresh confirms the runAttach refresh gate: lifecycle events
// trigger a refresh, pure log/heartbeat kinds are skipped, and an undecodable
// frame defaults to refreshing (fail safe, not silent-stale).
func TestEventTriggersRefresh(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"kind":"task.done","task_id":"t1"}`, true},
		{`{"kind":"task.claimed","task_id":"t1"}`, true},
		{`{"kind":"plan.imported"}`, true},
		{`{"kind":"worker.completed"}`, true},
		{`{"kind":"tick"}`, false},
		{`{"kind":"task.progress","task_id":"t1"}`, false},
		{`{not json`, true},        // undecodable → refresh (fail safe)
		{`{"task_id":"t1"}`, true}, // empty kind → refresh (fail safe)
	}
	for _, tc := range cases {
		if got := eventTriggersRefresh([]byte(tc.raw)); got != tc.want {
			t.Errorf("eventTriggersRefresh(%s) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestRunAttach_ReconnectsAfterStreamEnds is the regression for the GUI audit's
// C1: the live attach subscription was single-shot — Attach returning (a failed
// pre-supervisor dial, or an EOF when the supervisor restarts) killed the stream
// permanently for the rest of the session. runAttach must re-dial in a loop, so
// the event stream recovers after a supervisor blip.
func TestRunAttach_ReconnectsAfterStreamEnds(t *testing.T) {
	f := newFakeController()
	// Attach returns immediately every call — as if the supervisor is down / the
	// stream keeps ending. runAttach must keep re-dialing.
	f.attachReturn = context.Canceled

	u := newTestUI(t, f)
	// Shrink the retry delay on THIS ui only (per-instance field — no shared
	// global to race another test's runAttach goroutine).
	u.attachRetryDelay = 1 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { u.runAttach(ctx); close(done) }()

	// Poll until Attach has been called several times (proving the re-dial loop),
	// then cancel and confirm runAttach returns.
	deadline := time.After(3 * time.Second)
	for f.attachCount.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("Attach called only %d time(s) — runAttach did not reconnect after the stream ended", f.attachCount.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runAttach did not return after ctx cancel")
	}
}
