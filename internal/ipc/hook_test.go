package ipc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookIPCIsVersionedStrictAndSecretBlind(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "hook.sock")
	handler := &hookTestHandler{}
	server, err := NewServer(ServerOptions{
		SocketPath: endpoint, HeartbeatPath: endpoint + ".alive", Handler: handler,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Stop() }()

	const secret = "ghp_hook-wire-canary"
	for _, tc := range []struct {
		name    string
		version int
		args    string
		code    string
	}{
		{
			name: "old protocol cannot guess new command", version: HookProtoVersion - 1,
			args: `{"adapter":"claude","event":"Stop","session_id":"s"}`,
			code: CodeUnsupportedCommand,
		},
		{
			name: "unknown secret-bearing field is rejected", version: HookProtoVersion,
			args: `{"adapter":"claude","event":"Stop","session_id":"s","token":"` + secret + `"}`,
			code: CodeInvalidArgs,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := Dial(endpoint, 0)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			defer func() { _ = client.Close() }()
			if err := client.send(context.Background(), Request{
				Cmd: CmdHookEvent, ProtoVersion: tc.version, Args: json.RawMessage(tc.args),
			}); err != nil {
				t.Fatalf("send: %v", err)
			}
			response, err := client.readResponse(context.Background())
			if err != nil {
				t.Fatalf("readResponse: %v", err)
			}
			if response.Ok || response.Code != tc.code {
				t.Fatalf("response = %+v", response)
			}
			if strings.Contains(response.Error, secret) {
				t.Fatalf("response echoed secret: %q", response.Error)
			}
		})
	}
	if handler.calls != 0 {
		t.Fatalf("invalid requests reached handler %d times", handler.calls)
	}
}

type hookTestHandler struct{ calls int }

func (h *hookTestHandler) HandleHookEvent(context.Context, HookEventArgs) (HookEventReply, error) {
	h.calls++
	return HookEventReply{Allow: true, Reason: "ok"}, nil
}

func (*hookTestHandler) HandleStatus(context.Context) (StatusReply, error) {
	return StatusReply{}, nil
}

func (*hookTestHandler) HandleEnqueue(context.Context, EnqueueArgs) (EnqueueReply, error) {
	return EnqueueReply{}, nil
}

func (*hookTestHandler) HandleStop(context.Context, StopArgs) error { return nil }

func (*hookTestHandler) HandleReloadConfig(context.Context) error { return nil }

func (*hookTestHandler) HandleAttach(context.Context, AttachArgs, func(json.RawMessage) error) error {
	return nil
}
