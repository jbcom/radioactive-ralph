package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/observe"
)

type queryFakeHandler struct {
	fakeHandler

	snapshotReply *ObserveSnapshotReply
	snapshotErr   error
	messagesReply *ObserveMessagesReply
	messagesErr   error
	gotSnapshot   ObserveSnapshotArgs
	gotMessages   ObserveMessagesArgs
}

func (h *queryFakeHandler) HandleObserveSnapshot(
	_ context.Context,
	args ObserveSnapshotArgs,
) (*ObserveSnapshotReply, error) {
	h.gotSnapshot = args
	return h.snapshotReply, h.snapshotErr
}

func (h *queryFakeHandler) HandleObserveMessages(
	_ context.Context,
	args ObserveMessagesArgs,
) (*ObserveMessagesReply, error) {
	h.gotMessages = args
	return h.messagesReply, h.messagesErr
}

func TestQuerySnapshotAndMessagesRoundTrip(t *testing.T) {
	captured := time.Date(2026, time.July, 26, 18, 0, 0, 0, time.UTC)
	h := &queryFakeHandler{
		snapshotReply: &ObserveSnapshotReply{
			SchemaVersion: observe.SchemaVersion,
			CapturedAt:    captured,
			Project:       observe.Project{ID: "project-1"},
			Summary: observe.Summary{
				ActiveWorkerCount: 1,
			},
			Plans:        observe.PlanPage{Items: []observe.Plan{}},
			Tasks:        observe.TaskPage{Items: []observe.Task{}},
			Workers:      []observe.Worker{},
			RecentEvents: observe.EventPage{Items: []observe.Event{}},
		},
		messagesReply: &ObserveMessagesReply{
			SchemaVersion: observe.SchemaVersion,
			Items:         []observe.MessageMetadata{},
			HasMore:       true,
			NextAfterID:   9,
		},
	}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()

	snapshotClient := dialTest(t, sock)
	snapshot, err := snapshotClient.ObserveSnapshot(
		context.Background(),
		ObserveSnapshotArgs{
			ProjectID:     "project-1",
			PlanLimit:     4,
			PlanAfterID:   "plan-cursor",
			TaskLimit:     5,
			TaskAfter:     observe.TaskCursor{PlanID: "plan-1", TaskID: "task-1"},
			EventLimit:    6,
			EventBeforeID: 7,
		},
	)
	_ = snapshotClient.Close()
	if err != nil {
		t.Fatalf("ObserveSnapshot: %v", err)
	}
	if snapshot == nil || snapshot.CapturedAt != captured ||
		snapshot.Project.ID != "project-1" {
		t.Fatalf("snapshot reply = %+v", snapshot)
	}
	if h.gotSnapshot.PlanLimit != 4 ||
		h.gotSnapshot.TaskAfter.TaskID != "task-1" ||
		h.gotSnapshot.EventBeforeID != 7 {
		t.Fatalf("snapshot args not forwarded: %+v", h.gotSnapshot)
	}

	messageClient := dialTest(t, sock)
	messages, err := messageClient.ObserveMessages(
		context.Background(),
		ObserveMessagesArgs{
			ProjectID: "project-1",
			PlanID:    "plan-1",
			TaskID:    "task-1",
			AfterID:   8,
			Limit:     9,
		},
	)
	_ = messageClient.Close()
	if err != nil {
		t.Fatalf("ObserveMessages: %v", err)
	}
	if messages == nil || !messages.HasMore || messages.NextAfterID != 9 {
		t.Fatalf("messages reply = %+v", messages)
	}
	if h.gotMessages.AfterID != 8 || h.gotMessages.Limit != 9 {
		t.Fatalf("message args not forwarded: %+v", h.gotMessages)
	}
}

// TestQueryNewClientOldHandler proves a rolling v3 client never falls back to
// direct SQLite when the connected supervisor only implements v1/v2.
func TestQueryNewClientOldHandler(t *testing.T) {
	sock, _, cleanup := startServer(t, &fakeHandler{})
	defer cleanup()

	client := dialTest(t, sock)
	defer func() { _ = client.Close() }()
	reply, err := client.ObserveSnapshot(
		context.Background(),
		ObserveSnapshotArgs{ProjectID: "project-1"},
	)
	if reply != nil || !IsCode(err, CodeUnsupportedCommand) {
		t.Fatalf(
			"ObserveSnapshot against old handler = (%+v, %v), want nil unsupported_command",
			reply,
			err,
		)
	}
}

// TestQueryOldClientNewSupervisor proves an omitted pre-versioned protocol
// value still reaches the original v1 status command on a v3 supervisor.
func TestQueryOldClientNewSupervisor(t *testing.T) {
	h := &queryFakeHandler{
		fakeHandler: fakeHandler{
			statusReply: StatusReply{PID: 42, ProtoVersion: ProtoVersion},
		},
	}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()

	client := dialTest(t, sock)
	defer func() { _ = client.Close() }()
	if err := client.send(
		context.Background(),
		Request{Cmd: CmdStatus},
	); err != nil {
		t.Fatalf("send legacy status: %v", err)
	}
	resp, err := client.readResponse(context.Background())
	if err != nil {
		t.Fatalf("read legacy status: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("legacy status response = %+v", resp)
	}
	var status StatusReply
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		t.Fatalf("decode legacy status: %v", err)
	}
	if status.PID != 42 || status.ProtoVersion != ProtoVersion {
		t.Fatalf("legacy status payload = %+v", status)
	}
}

func TestQueryRejectsMissingV3ProtocolTag(t *testing.T) {
	h := &queryFakeHandler{
		snapshotReply: &ObserveSnapshotReply{
			SchemaVersion: observe.SchemaVersion,
		},
	}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()

	client := dialTest(t, sock)
	defer func() { _ = client.Close() }()
	body, err := json.Marshal(ObserveSnapshotArgs{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	if err := client.send(context.Background(), Request{
		Cmd:  CmdObserveSnapshot,
		Args: body,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	resp, err := client.readResponse(context.Background())
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.Ok || resp.Code != CodeUnsupportedCommand {
		t.Fatalf("missing-version query response = %+v, want unsupported_command", resp)
	}
	if h.gotSnapshot.ProjectID != "" {
		t.Fatalf("query handler was called despite missing v3 tag: %+v", h.gotSnapshot)
	}
}

func TestQueryCodedErrorReachesClient(t *testing.T) {
	h := &queryFakeHandler{
		snapshotErr: &codedFakeErr{
			code: CodeInvalidArgs,
			msg:  "invalid cursor",
		},
	}
	sock, _, cleanup := startServer(t, h)
	defer cleanup()

	client := dialTest(t, sock)
	defer func() { _ = client.Close() }()
	reply, err := client.ObserveSnapshot(
		context.Background(),
		ObserveSnapshotArgs{ProjectID: "project-1"},
	)
	if reply != nil || !IsCode(err, CodeInvalidArgs) {
		t.Fatalf("ObserveSnapshot = (%+v, %v), want nil invalid_args", reply, err)
	}
}

func TestClientUsesCommandScopedProtocolVersions(t *testing.T) {
	drive := captureClientRequest(t, func(client *Client) error {
		_, err := client.PlanSetStatus(
			context.Background(),
			PlanSetStatusArgs{PlanID: "plan-1", Status: "paused"},
		)
		return err
	})
	if drive.ProtoVersion != DriveProtoVersion {
		t.Fatalf(
			"drive proto = %d, want rolling-compatible v%d",
			drive.ProtoVersion,
			DriveProtoVersion,
		)
	}

	query := captureClientRequest(t, func(client *Client) error {
		_, err := client.ObserveSnapshot(
			context.Background(),
			ObserveSnapshotArgs{ProjectID: "project-1"},
		)
		return err
	})
	if query.ProtoVersion != QueryProtoVersion {
		t.Fatalf(
			"query proto = %d, want v%d",
			query.ProtoVersion,
			QueryProtoVersion,
		)
	}

	attach := captureClientRequest(t, func(client *Client) error {
		return client.AttachEvents(
			context.Background(),
			AttachArgs{ProjectID: "project-1"},
			func(AttachEvent) error { return nil },
		)
	})
	if attach.ProtoVersion != AttachProtoVersion {
		t.Fatalf(
			"attach proto = %d, want safe-frame v%d",
			attach.ProtoVersion,
			AttachProtoVersion,
		)
	}
}

func captureClientRequest(
	t *testing.T,
	call func(*Client) error,
) Request {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	client := &Client{
		conn:   clientConn,
		reader: bufio.NewReader(clientConn),
	}
	type serverResult struct {
		request Request
		err     error
	}
	resultCh := make(chan serverResult, 1)
	go func() {
		defer func() { _ = serverConn.Close() }()
		var request Request
		if err := json.NewDecoder(serverConn).Decode(&request); err != nil {
			resultCh <- serverResult{err: err}
			return
		}
		err := json.NewEncoder(serverConn).Encode(Response{
			Ok:   true,
			Data: json.RawMessage(`{}`),
		})
		resultCh <- serverResult{request: request, err: err}
	}()

	callErr := call(client)
	_ = client.Close()
	result := <-resultCh
	if callErr != nil {
		t.Fatalf("client call: %v", callErr)
	}
	if result.err != nil {
		t.Fatalf("serve client call: %v", result.err)
	}
	return result.request
}
