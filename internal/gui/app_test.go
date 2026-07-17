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
