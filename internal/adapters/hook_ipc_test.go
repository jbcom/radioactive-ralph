package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

func TestRunHookSendsOnlyFiniteNormalizedFields(t *testing.T) {
	endpoint, heartbeat := ipc.ServiceEndpoint(t.TempDir())
	handler := &recordingHookHandler{reply: ipc.HookEventReply{
		Allow: true, Reason: "progress_recorded",
	}}
	server, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath: endpoint, HeartbeatPath: heartbeat, Handler: handler,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Stop() }()

	const secret = "ghp_normalization-canary"
	lookup := func(key string) string {
		switch key {
		case ManagedSessionEnv:
			return "managed-session"
		case HookEndpointEnv:
			return endpoint
		default:
			return ""
		}
	}
	var stdout, stderr bytes.Buffer
	err = RunHook(context.Background(), "claude", "PostToolUse",
		bytes.NewBufferString(`{"hook_event_name":"PostToolUse","tool_result":"`+secret+`"}`),
		&stdout, &stderr, lookup)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if handler.got != (ipc.HookEventArgs{
		Adapter: "claude", Event: "PostToolUse", SessionID: "managed-session",
	}) {
		t.Fatalf("normalized args = %+v", handler.got)
	}
	encoded, err := json.Marshal(handler.got)
	if err != nil {
		t.Fatalf("marshal normalized args: %v", err)
	}
	if bytes.Contains(encoded, []byte(secret)) || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("hook leaked provider payload: args=%s stdout=%q stderr=%q", encoded, stdout.String(), stderr.String())
	}
}

type recordingHookHandler struct {
	reply ipc.HookEventReply
	got   ipc.HookEventArgs
	calls int
}

func (h *recordingHookHandler) HandleHookEvent(_ context.Context, args ipc.HookEventArgs) (ipc.HookEventReply, error) {
	h.calls++
	h.got = args
	return h.reply, nil
}

func (*recordingHookHandler) HandleStatus(context.Context) (ipc.StatusReply, error) {
	return ipc.StatusReply{}, nil
}

func (*recordingHookHandler) HandleEnqueue(context.Context, ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	return ipc.EnqueueReply{}, nil
}

func (*recordingHookHandler) HandleStop(context.Context, ipc.StopArgs) error { return nil }

func (*recordingHookHandler) HandleReloadConfig(context.Context) error { return nil }

func (*recordingHookHandler) HandleAttach(context.Context, ipc.AttachArgs, func(json.RawMessage) error) error {
	return nil
}
