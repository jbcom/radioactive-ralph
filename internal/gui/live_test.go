package gui

import (
	"context"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

// TestLiveController_ReadAndDriveRoundTrip is the one GUI test that exercises
// real I/O: it starts a real supervisor and asserts that a READ (Status) and a
// DRIVE (ImportPlan) both round-trip through the socket via liveController. The
// rest of the GUI tests use the in-memory fake. Untagged so it runs in the
// normal CGO-off test job — liveController imports no Fyne.
func TestLiveController_ReadAndDriveRoundTrip(t *testing.T) {
	stateRoot := t.TempDir()
	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(stateRoot + "/store.db")})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	projectID, err := st.CreateProject(context.Background(), "gui-proj", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx, supervisor.Options{RuntimeDir: stateRoot, Store: st}) }()

	// Wait until the supervisor is reachable.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, ferr := supervisor.Find(stateRoot); ferr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctrl := NewLiveController(stateRoot, st, projectID)

	// READ: Status round-trips.
	if _, err := ctrl.Status(context.Background()); err != nil {
		t.Fatalf("liveController.Status: %v", err)
	}

	// DRIVE: ImportPlan round-trips and lands the plan active in the store.
	reply, err := ctrl.ImportPlan(context.Background(), ipc.PlanImportArgs{
		Markdown: "# Via GUI\n\n1. step\n", Project: projectID,
	})
	if err != nil {
		t.Fatalf("liveController.ImportPlan: %v", err)
	}
	if reply.Title != "Via GUI" {
		t.Errorf("import reply title = %q, want Via GUI", reply.Title)
	}
	plans, err := st.ListPlans(context.Background(), projectID, nil)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 1 || plans[0].Status != store.PlanStatusActive {
		t.Fatalf("plan not imported active via GUI controller: %+v", plans)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not exit within 3s")
	}
}
