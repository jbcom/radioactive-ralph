package ipc

import (
	"context"
	"testing"
	"time"
)

// driveFakeHandler embeds fakeHandler (v1) and adds the v2 DriveHandler
// methods, recording calls and returning scripted results.
type driveFakeHandler struct {
	fakeHandler

	importReply  PlanImportReply
	importErr    error
	setStatusErr error
	approveErr   error
	killErr      error
	gotImport    PlanImportArgs
	gotSetStatus PlanSetStatusArgs
	gotApprove   TaskApproveArgs
	gotKill      WorkerKillArgs
}

func (h *driveFakeHandler) HandlePlanImport(_ context.Context, a PlanImportArgs) (PlanImportReply, error) {
	h.gotImport = a
	return h.importReply, h.importErr
}
func (h *driveFakeHandler) HandlePlanSetStatus(_ context.Context, a PlanSetStatusArgs) (PlanSetStatusReply, error) {
	h.gotSetStatus = a
	return PlanSetStatusReply(a), h.setStatusErr
}
func (h *driveFakeHandler) HandleTaskApprove(_ context.Context, a TaskApproveArgs) error {
	h.gotApprove = a
	return h.approveErr
}
func (h *driveFakeHandler) HandleWorkerKill(_ context.Context, a WorkerKillArgs) error {
	h.gotKill = a
	return h.killErr
}

func dialTest(t *testing.T, socketPath string) *Client {
	t.Helper()
	c, err := Dial(socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return c
}

func TestDrive_PlanImportRoundTrip(t *testing.T) {
	h := &driveFakeHandler{importReply: PlanImportReply{PlanID: "p1", Slug: "ship", Title: "Ship"}}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()

	c := dialTest(t, sock)
	defer func() { _ = c.Close() }()

	reply, err := c.PlanImport(context.Background(), PlanImportArgs{Markdown: "# Ship\n", Project: "proj"})
	if err != nil {
		t.Fatalf("PlanImport: %v", err)
	}
	if reply.PlanID != "p1" || reply.Slug != "ship" {
		t.Errorf("reply = %+v, want p1/ship", reply)
	}
	if h.gotImport.Project != "proj" || h.gotImport.Markdown != "# Ship\n" {
		t.Errorf("handler got %+v, args not forwarded", h.gotImport)
	}
}

func TestDrive_SetStatusApproveKillRoundTrip(t *testing.T) {
	h := &driveFakeHandler{}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()
	ctx := context.Background()

	// Each IPC call is a one-shot connection (the server closes the conn after
	// one request/response), so dial fresh per command — the same contract the
	// TUI's live data source uses.
	c1 := dialTest(t, sock)
	if _, err := c1.PlanSetStatus(ctx, PlanSetStatusArgs{PlanID: "p", Status: "paused"}); err != nil {
		t.Fatalf("PlanSetStatus: %v", err)
	}
	_ = c1.Close()
	if h.gotSetStatus.Status != "paused" {
		t.Errorf("set-status not forwarded: %+v", h.gotSetStatus)
	}

	c2 := dialTest(t, sock)
	if err := c2.TaskApprove(ctx, TaskApproveArgs{PlanID: "p", TaskID: "t"}); err != nil {
		t.Fatalf("TaskApprove: %v", err)
	}
	_ = c2.Close()
	if h.gotApprove.TaskID != "t" {
		t.Errorf("approve not forwarded: %+v", h.gotApprove)
	}

	c3 := dialTest(t, sock)
	if err := c3.WorkerKill(ctx, WorkerKillArgs{WorkerID: "w"}); err != nil {
		t.Fatalf("WorkerKill: %v", err)
	}
	_ = c3.Close()
	if h.gotKill.WorkerID != "w" {
		t.Errorf("kill not forwarded: %+v", h.gotKill)
	}
}

// codedFakeErr is a handler error carrying an ipc code.
type codedFakeErr struct{ code, msg string }

func (e *codedFakeErr) Error() string { return e.msg }
func (e *codedFakeErr) Code() string  { return e.code }

func TestDrive_CodedErrorSurfacesToClient(t *testing.T) {
	h := &driveFakeHandler{killErr: &codedFakeErr{code: CodeNotFound, msg: "worker w not found"}}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()
	c := dialTest(t, sock)
	defer func() { _ = c.Close() }()

	err := c.WorkerKill(context.Background(), WorkerKillArgs{WorkerID: "w"})
	if err == nil {
		t.Fatal("WorkerKill: want error, got nil")
	}
	if !IsCode(err, CodeNotFound) {
		t.Errorf("err = %v, want a CodedError with CodeNotFound", err)
	}
}

// TestDrive_UnsupportedByV1Handler proves a v1-only handler (no DriveHandler)
// answers a drive command with an unsupported_command coded error, so an older
// supervisor fails cleanly against a newer client.
func TestDrive_UnsupportedByV1Handler(t *testing.T) {
	// fakeHandler implements only the v1 Handler, not DriveHandler.
	sock, _, cleanup := startServer(t, &fakeHandler{})
	defer cleanup()
	c := dialTest(t, sock)
	defer func() { _ = c.Close() }()

	_, err := c.PlanImport(context.Background(), PlanImportArgs{Markdown: "# x", Project: "p"})
	if err == nil {
		t.Fatal("PlanImport against a v1 handler: want error, got nil")
	}
	if !IsCode(err, CodeUnsupportedCommand) {
		t.Errorf("err = %v, want CodeUnsupportedCommand", err)
	}
}

// TestDrive_UnknownCommand confirms a truly unknown command returns
// unsupported_command too.
func TestDrive_UnknownCommand(t *testing.T) {
	sock, _, cleanup := startServer(t, &driveFakeHandler{})
	defer cleanup()
	c := dialTest(t, sock)
	defer func() { _ = c.Close() }()

	// Send a raw unknown command via the low-level driveCall.
	err := c.driveCall(context.Background(), "bogus-command", struct{}{}, nil)
	if !IsCode(err, CodeUnsupportedCommand) {
		t.Errorf("unknown command err = %v, want CodeUnsupportedCommand", err)
	}
}
