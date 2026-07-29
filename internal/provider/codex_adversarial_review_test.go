package provider

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestReviewExactCodex0145Messages(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    codexFailureCategory
	}{
		{
			name:    "context window",
			message: "Codex ran out of room in the model's context window. Start a new thread or clear earlier history before retrying.",
			want:    codexFailureInvalidRequest,
		},
		{
			name:    "server overloaded",
			message: "Selected model is at capacity. Please try a different model.",
			want:    codexFailureService,
		},
		{
			name:    "internal server error",
			message: "We're currently experiencing high demand, which may cause temporary errors.",
			want:    codexFailureService,
		},
		{
			name:    "refresh token expired",
			message: "Your access token could not be refreshed because your refresh token has expired. Please log out and sign in again.",
			want:    codexFailureAuthentication,
		},
		{
			name:    "workspace credits",
			message: "Your workspace is out of credits. Add credits to continue.",
			want:    codexFailureQuota,
		},
		{
			name:    "workspace spend cap",
			message: "You hit your spend cap set in your workspace. Increase your spend cap to continue.",
			want:    codexFailureQuota,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyCodexFailure(tc.message); got != tc.want {
				t.Errorf("classifyCodexFailure(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}

func BenchmarkReviewDuplicateDiagnosticFloodBeforeFull(b *testing.B) {
	var diagnostics codexDiagnosticCollector
	frame := []byte(`{"type":"error","message":"authentication failed ` +
		strings.Repeat("x", (1<<20)-128) + `"}`)
	diagnostics.consume(frame)
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for range b.N {
		diagnostics.consume(frame)
	}
}

func BenchmarkReviewDiagnosticFloodAfterFull(b *testing.B) {
	var diagnostics codexDiagnosticCollector
	for _, frame := range []string{
		`{"type":"error","message":"authentication failed"}`,
		`{"type":"error","message":"model unavailable"}`,
		`{"type":"error","message":"quota exhausted"}`,
		`{"type":"error","message":"rate limit exceeded"}`,
		`{"type":"error","message":"network timeout"}`,
		`{"type":"error","message":"service unavailable"}`,
		`{"type":"error","message":"invalid request"}`,
		`{"type":"error","message":"unclassified"}`,
	} {
		diagnostics.consume([]byte(frame))
	}
	frame := []byte(`{"type":"error","message":"authentication failed ` +
		strings.Repeat("x", (1<<20)-128) + `"}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for range b.N {
		diagnostics.consume(frame)
	}
}

func TestReviewCodexCancellationPreservesContextError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-review-context.sh", `#!/bin/sh
printf '%s\n' '{"type":"turn.started"}'
sleep 300
`)
	for i := range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(time.Millisecond)
			cancel()
		}()
		_, err := CodexRunner{}.Run(ctx, Binding{
			Name:            "codex",
			BinaryFromLocal: true,
			Config:          BindingConfig{Type: "codex", Binary: bin},
		}, Request{WorkingDir: t.TempDir()})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d error = %v, want wrapping context.Canceled", i, err)
		}
	}
}

func TestReviewCodex0145MaxCommandEventDoesNotFalseStall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-review-large-jsonl.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then
    out="$arg"
  fi
  previous="$arg"
done
printf '%s' '{"outcome":"done","summary":"large event consumed","evidence":[]}' > "$out"
# Emit the opening bytes from /bin/sh BEFORE paying python3 interpreter
# startup. The stall clock starts at the first byte of provider output, and
# this test sets DefaultStallTimeout = 2s -- so with the whole line produced
# by python3, exec-to-first-byte INCLUDED interpreter startup and the test
# was measuring python3 launch latency, which it never intended to measure.
# It false-stalled under parallel load:
#   "agent blocked (killed by watchdog): no output before stall timeout"
# in a test whose entire purpose is proving a large event does NOT stall.
# The event emitted is byte-for-byte identical; only the ORDER changed.
printf '%s' '{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"'
python3 -c 'import sys; sys.stdout.write("\\n" * (1 << 20)); sys.stdout.flush()'
printf '%s\n' '"}}'
`)
	originalStall := DefaultStallTimeout
	DefaultStallTimeout = 2 * time.Second
	defer func() { DefaultStallTimeout = originalStall }()

	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("CodexRunner.Run rejected a clean 0.145-sized JSONL event: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "large event consumed") {
		t.Fatalf("AssistantOutput = %q, want last-message result", result.AssistantOutput)
	}
}
