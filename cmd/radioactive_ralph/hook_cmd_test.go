package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func TestHookCommandReturnsBlockSentinelWithoutEcho(t *testing.T) {
	t.Setenv(adapters.ManagedSessionEnv, "session-canary")
	t.Setenv(adapters.HookEndpointEnv, "")
	cmd := newHookCmd()
	cmd.SetArgs([]string{"event", "--adapter", "claude", "--event", "Stop"})
	cmd.SetIn(strings.NewReader(`{"hook_event_name":"Stop","token":"secret-canary"}`))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	if !errors.Is(err, adapters.ErrBlocked) {
		t.Fatalf("Execute error = %v, want ErrBlocked", err)
	}
	output := stdout.String() + stderr.String()
	if strings.Contains(output, "secret-canary") || strings.Contains(output, "session-canary") {
		t.Fatalf("hook output echoed input/environment: %q", output)
	}
}
