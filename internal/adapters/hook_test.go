package adapters

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

func TestRunHookUnmanagedIsNoopWithoutReadingSecretPayload(t *testing.T) {
	const secret = "ghp_hook-payload-canary"
	reader := &panicReader{}
	var stdout, stderr bytes.Buffer
	err := RunHook(context.Background(), "claude", "PostToolUse", reader,
		&stdout, &stderr, func(key string) string {
			if key == HookEndpointEnv {
				return secret
			}
			return ""
		})
	if err != nil {
		t.Fatalf("unmanaged RunHook: %v", err)
	}
	if reader.read {
		t.Fatal("unmanaged hook read provider payload")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unmanaged hook emitted output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunHookUnmanagedInvalidEventRemainsNoop(t *testing.T) {
	reader := &panicReader{}
	var stdout, stderr bytes.Buffer
	if err := RunHook(context.Background(), "unknown", "unknown", reader,
		&stdout, &stderr, func(string) string { return "" }); err != nil {
		t.Fatalf("unmanaged invalid event: %v", err)
	}
	if reader.read || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("unmanaged invalid event was not a strict no-op")
	}
}

func TestRunHookManagedFailureIsStaticAndFailClosed(t *testing.T) {
	const secret = "ghp_hook-payload-canary"
	lookup := func(key string) string {
		if key == ManagedSessionEnv {
			return "managed-session"
		}
		return "" // no endpoint: deterministic fail-closed path
	}
	for _, adapter := range []string{"claude", "codex", "opencode"} {
		t.Run(adapter, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := RunHook(context.Background(), adapter, "Stop",
				strings.NewReader(`{"hook_event_name":"Stop","tool_result":"`+secret+`"}`),
				&stdout, &stderr, lookup)
			output := stdout.String() + stderr.String()
			if strings.Contains(output, secret) || strings.Contains(output, "managed-session") {
				t.Fatalf("blocked output echoed input/environment: %q", output)
			}
			assertExactBlockProtocol(
				t, adapter, "invalid_event", err, stdout.String(), stderr.String())
		})
	}
}

func TestBlockUsesExactProviderProtocols(t *testing.T) {
	for _, adapter := range []string{"claude", "codex", "opencode"} {
		t.Run(adapter, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := block(adapter, &stdout, &stderr, "verification_failed")
			assertExactBlockProtocol(t, adapter, "verification_failed", err, stdout.String(), stderr.String())
		})
	}
}

func TestRunHookBlockedAndUnavailableSupervisorUseExactProviderProtocols(t *testing.T) {
	blockedEndpoint, heartbeat := ipc.ServiceEndpoint(t.TempDir())
	server, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath:    blockedEndpoint,
		HeartbeatPath: heartbeat,
		Handler: &recordingHookHandler{reply: ipc.HookEventReply{
			Allow: false, Reason: "verification_failed",
		}},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Stop() }()

	unavailableEndpoint, _ := ipc.ServiceEndpoint(t.TempDir())
	for _, scenario := range []struct {
		name, endpoint, reason string
	}{
		{name: "blocked verdict", endpoint: blockedEndpoint, reason: "verification_failed"},
		{name: "unavailable supervisor", endpoint: unavailableEndpoint, reason: "supervisor_unavailable"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, adapter := range []string{"claude", "codex", "opencode"} {
				t.Run(adapter, func(t *testing.T) {
					lookup := func(key string) string {
						switch key {
						case ManagedSessionEnv:
							return "managed-session"
						case HookEndpointEnv:
							return scenario.endpoint
						default:
							return ""
						}
					}
					var stdout, stderr bytes.Buffer
					err := RunHook(context.Background(), adapter, "Stop",
						strings.NewReader(`{"hook_event_name":"Stop"}`),
						&stdout, &stderr, lookup)
					assertExactBlockProtocol(
						t, adapter, scenario.reason, err, stdout.String(), stderr.String())
				})
			}
		})
	}
}

func assertExactBlockProtocol(
	t *testing.T,
	adapter, reasonCode string,
	err error,
	stdout, stderr string,
) {
	t.Helper()
	const reason = "Radioactive Ralph verification is not complete.\n"
	switch adapter {
	case "claude":
		if !errors.Is(err, ErrBlocked) || stdout != "" || stderr != reason {
			t.Fatalf("Claude block = err:%v stdout:%q stderr:%q", err, stdout, stderr)
		}
	case "codex":
		want := `{"decision":"block","reason":"Radioactive Ralph verification is not complete."}` + "\n"
		if err != nil || stdout != want || stderr != "" {
			t.Fatalf("Codex block = err:%v stdout:%q stderr:%q", err, stdout, stderr)
		}
	case "opencode":
		want := `{"status":"` + reasonCode + `"}` + "\n"
		if !errors.Is(err, ErrBlocked) || stdout != want || stderr != "" {
			t.Fatalf("OpenCode block = err:%v stdout:%q stderr:%q", err, stdout, stderr)
		}
	default:
		t.Fatalf("unexpected adapter %q", adapter)
	}
}

type panicReader struct{ read bool }

func (r *panicReader) Read([]byte) (int, error) {
	r.read = true
	panic("unmanaged hook must not read stdin")
}
