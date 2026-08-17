package adapters

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunHookUnmanagedIsNoopWithoutReadingSecretPayload(t *testing.T) {
	const secret = "ghp_hook-payload-canary"
	reader := &panicReader{}
	var stdout, stderr bytes.Buffer
	err := RunHook(context.Background(), "claude", "PostToolUse", reader,
		&stdout, &stderr, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unmanaged RunHook: %v", err)
	}
	if reader.read {
		t.Fatal("unmanaged hook read provider payload")
	}
	if strings.Contains(stdout.String()+stderr.String(), secret) {
		t.Fatal("unmanaged hook echoed secret")
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
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("RunHook error = %v, want ErrBlocked", err)
			}
			output := stdout.String() + stderr.String()
			if strings.Contains(output, secret) || strings.Contains(output, "managed-session") {
				t.Fatalf("blocked output echoed input/environment: %q", output)
			}
			if output == "" {
				t.Fatal("blocked hook emitted no finite status")
			}
		})
	}
}

type panicReader struct{ read bool }

func (r *panicReader) Read([]byte) (int, error) {
	r.read = true
	panic("unmanaged hook must not read stdin")
}
