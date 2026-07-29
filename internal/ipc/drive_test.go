package ipc

import (
	"context"
	"testing"
	"time"
)

// driveFakeHandler embeds fakeHandler (v1) and adds the v2 DriveHandler
// methods, recording calls and returning scripted results.
type driveFakeHandler struct {
	gotProjectEnsure ProjectEnsureArgs
	projectEnsureErr error
	fakeHandler

	importReply  PlanImportReply
	importErr    error
	setStatusErr error
	deletedPlan  string
	approveErr   error
	killErr      error
	gotImport    PlanImportArgs
	gotSetStatus PlanSetStatusArgs
	gotApprove   TaskApproveArgs
	gotKill      WorkerKillArgs

	configGetReply ProjectConfigGetReply
	configGetErr   error
	configApplyErr error
	gotConfigGet   ProjectConfigGetArgs
	gotConfigApply ProjectConfigApplyArgs
}

func (h *driveFakeHandler) HandleProjectConfigGet(_ context.Context, a ProjectConfigGetArgs) (ProjectConfigGetReply, error) {
	h.gotConfigGet = a
	return h.configGetReply, h.configGetErr
}

func (h *driveFakeHandler) HandleProjectConfigApply(_ context.Context, a ProjectConfigApplyArgs) error {
	h.gotConfigApply = a
	return h.configApplyErr
}

func (h *driveFakeHandler) HandlePlanImport(_ context.Context, a PlanImportArgs) (PlanImportReply, error) {
	h.gotImport = a
	return h.importReply, h.importErr
}
func (h *driveFakeHandler) HandlePlanDelete(_ context.Context, a PlanDeleteArgs) (PlanDeleteReply, error) {
	h.deletedPlan = a.PlanID
	return PlanDeleteReply(a), nil
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

// HandleProjectEnsure satisfies DriveHandler for the fake.
func (h *driveFakeHandler) HandleProjectEnsure(
	_ context.Context,
	args ProjectEnsureArgs,
) (*ProjectEnsureReply, error) {
	h.gotProjectEnsure = args
	if h.projectEnsureErr != nil {
		return nil, h.projectEnsureErr
	}
	return &ProjectEnsureReply{ProjectID: "project-fake", Created: false}, nil
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

// TestDrive_RejectsNewerProtocolVersion proves the server refuses a drive
// request whose proto_version is newer than it speaks, BEFORE routing it to a
// handler. A future v3 could reuse a v2 command name with changed payload
// semantics; without this guard the server would decode-and-act as v2. The
// rejection is a clean unsupported_command, and the (otherwise-succeeding)
// handler must never be invoked.
func TestDrive_RejectsNewerProtocolVersion(t *testing.T) {
	h := &driveFakeHandler{} // would succeed if reached
	sock, _, cleanup := startServer(t, h)
	defer cleanup()
	c := dialTest(t, sock)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	// Hand-send a drive request tagged with a version this server can't speak.
	if err := c.send(ctx, Request{Cmd: CmdWorkerKill, ProtoVersion: ProtoVersion + 1}); err != nil {
		t.Fatalf("send: %v", err)
	}
	resp, err := c.readResponse(ctx)
	if err != nil {
		t.Fatalf("readResponse: %v", err)
	}
	if resp.Ok {
		t.Fatal("newer-version drive request: want !Ok, got Ok")
	}
	if resp.Code != CodeUnsupportedCommand {
		t.Errorf("resp.Code = %q, want %q", resp.Code, CodeUnsupportedCommand)
	}
	if (h.gotKill != WorkerKillArgs{}) {
		t.Errorf("handler was invoked (gotKill=%+v), want the version guard to reject before dispatch", h.gotKill)
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

// TestDriveHandlerIsSatisfiedBySupervisor guards a hazard this file discovered
// the hard way.
//
// DriveHandler is OPTIONAL: the server detects it with a type assertion, so a
// Handler that misses even one method silently loses EVERY drive command --
// plan-import, task-approve, worker-kill, all of it -- with no compile error.
// Adding HandlePlanDelete did exactly that to the test fake here, and three
// unrelated round-trip tests failed with "unsupported_command".
//
// The same slip against the real supervisor would compile, ship, and disable
// the entire drive surface at runtime. A compile-time assertion turns that into
// a build failure instead.
func TestDriveHandlerIsSatisfiedBySupervisor(t *testing.T) {
	// The fake must satisfy it, or the round-trip tests below are testing a
	// server that silently refused every command.
	var _ DriveHandler = (*driveFakeHandler)(nil)

	// A Handler WITHOUT the drive methods must not satisfy it -- otherwise the
	// assertion above proves nothing about which methods are required.
	if _, ok := any(&fakeHandler{}).(DriveHandler); ok {
		t.Error("a bare Handler satisfies DriveHandler; the optional-interface " +
			"detection cannot distinguish a drive-capable server from one that " +
			"will refuse every command")
	}
}
