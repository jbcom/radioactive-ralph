package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
)

func TestDriveCommandProtoVersionIsStrict(t *testing.T) {
	tests := []struct {
		command string
		want    int
	}{
		{CmdStatus, 1},
		{CmdAttach, 1},
		{CmdEnqueue, 1},
		{CmdStop, 1},
		{CmdReloadConfig, 1},
		{CmdPlanImport, 2},
		{CmdPlanSetStatus, 2},
		{CmdTaskApprove, 2},
		{CmdWorkerKill, 2},
		{CmdTaskRetry, 3},
		{CmdTaskList, 3},
		{CmdCalibrationPut, 3},
		{CmdCalibrationGet, 3},
		{CmdCalibrationList, 3},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			got, err := commandProtoVersion(test.command)
			if err != nil {
				t.Fatalf("commandProtoVersion: %v", err)
			}
			if got != test.want {
				t.Errorf("version = %d, want %d", got, test.want)
			}
		})
	}
	if _, err := commandProtoVersion("future-drive-command"); !IsCode(err, CodeUnsupportedCommand) {
		t.Fatalf("unknown command error = %v, want CodeUnsupportedCommand", err)
	}
}

// TestV3ClientDriveCallsAgainstV2Supervisor proves rolling compatibility at
// the command boundary. The simulated supervisor implements the real version
// guard: an unchanged v2 command from this v3 client is served, while a v3-only
// command is rejected before execution.
func TestV3ClientDriveCallsAgainstV2Supervisor(t *testing.T) {
	t.Run("v1-attach-is-served", func(t *testing.T) {
		client, request := versionGuardedPipeClient(t, 2)
		defer func() { _ = client.Close() }()

		err := client.Attach(
			context.Background(),
			AttachArgs{ProjectID: "project"},
			func(json.RawMessage) error { return nil },
		)
		if err != nil {
			t.Fatalf("Attach against v2 supervisor: %v", err)
		}
		if request.ProtoVersion != 1 {
			t.Fatalf("proto_version = %d, want minimum v1", request.ProtoVersion)
		}
	})

	t.Run("unchanged-v2-command-is-served", func(t *testing.T) {
		client, request := versionGuardedPipeClient(t, 2)
		defer func() { _ = client.Close() }()

		err := client.TaskApprove(context.Background(), TaskApproveArgs{
			PlanID: "plan", TaskID: "task",
		})
		if err != nil {
			t.Fatalf("TaskApprove against v2 supervisor: %v", err)
		}
		if request.ProtoVersion != 2 {
			t.Fatalf("proto_version = %d, want minimum v2", request.ProtoVersion)
		}
	})

	t.Run("v3-only-command-is-rejected", func(t *testing.T) {
		client, request := versionGuardedPipeClient(t, 2)
		defer func() { _ = client.Close() }()

		err := client.TaskRetry(context.Background(), TaskRetryArgs{
			PlanID: "plan", TaskID: "task",
		})
		if !IsCode(err, CodeUnsupportedCommand) {
			t.Fatalf("TaskRetry error = %v, want CodeUnsupportedCommand", err)
		}
		if request.ProtoVersion != 3 {
			t.Fatalf("proto_version = %d, want v3", request.ProtoVersion)
		}
	})
}

func versionGuardedPipeClient(t *testing.T, serverVersion int) (*Client, *Request) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	request := &Request{}
	client := &Client{conn: clientConn, reader: bufio.NewReader(clientConn)}

	go func() {
		defer func() { _ = serverConn.Close() }()
		line, err := bufio.NewReader(serverConn).ReadBytes('\n')
		if err != nil {
			return
		}
		if err := json.Unmarshal(line, request); err != nil {
			return
		}
		response := Response{Ok: true}
		if request.ProtoVersion > serverVersion {
			response = Response{
				Ok: false, Code: CodeUnsupportedCommand,
				Error: fmt.Sprintf(
					"protocol version %d not supported (this supervisor speaks v%d)",
					request.ProtoVersion, serverVersion,
				),
			}
		}
		frame, err := encodeJSONLine(response)
		if err == nil {
			_, _ = serverConn.Write(frame)
		}
	}()
	return client, request
}
