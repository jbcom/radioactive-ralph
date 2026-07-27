//go:build !windows

package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBuiltinProvidersRenewStallWithoutExtendingTurnDeadline(t *testing.T) {
	tests := []struct {
		name         string
		frame        string
		runner       Runner
		turnTimeout  string
		stallTimeout string
		minElapsed   time.Duration
		maxElapsed   time.Duration
	}{
		// The first subprocess starts while `go test ./...` is launching every
		// package. Give that cold-start probe a production-realistic lease so
		// host scheduler contention is not mistaken for a provider stall.
		{
			name: "opencode", frame: `{"type":"step_start","sessionID":"s"}`,
			runner: OpencodeRunner{}, turnTimeout: "20s", stallTimeout: "15s",
			minElapsed: 18 * time.Second, maxElapsed: 25 * time.Second,
		},
		{
			name: "claude", frame: `{"type":"assistant","message":{"content":[]}}`,
			runner: ClaudeRunner{}, turnTimeout: "5s", stallTimeout: "2s",
			minElapsed: 4 * time.Second, maxElapsed: 8 * time.Second,
		},
		{
			name: "codex", frame: `{"type":"thread.started"}`,
			runner: CodexRunner{}, turnTimeout: "5s", stallTimeout: "2s",
			minElapsed: 4 * time.Second, maxElapsed: 8 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := writeFakeCLI(t, "fake-"+tt.name+"-progress.sh", `#!/bin/sh
while :; do
  printf '%s\n' '`+tt.frame+`'
  sleep 0.15
done
`)
			start := time.Now()
			_, err := tt.runner.Run(context.Background(), Binding{
				Name:            tt.name,
				BinaryFromLocal: true,
				Config: BindingConfig{
					Type:         tt.name,
					Binary:       bin,
					TurnTimeout:  tt.turnTimeout,
					StallTimeout: tt.stallTimeout,
				},
			}, Request{WorkingDir: t.TempDir(), UserPrompt: "work"})
			elapsed := time.Since(start)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want absolute turn deadline", err)
			}
			if errors.Is(err, ErrAgentBlocked) || errors.Is(err, ErrProviderStalled) {
				t.Fatalf("healthy progress was misclassified as a stall: %v", err)
			}
			if elapsed < tt.minElapsed || elapsed > tt.maxElapsed {
				t.Fatalf("elapsed = %s, want progress beyond stall but bounded by total", elapsed)
			}
		})
	}
}
