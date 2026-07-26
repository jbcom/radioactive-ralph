package provider

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

func TestClaudeRunnerRejectsUnsuccessfulResultFramesWithoutPayload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	tests := []struct {
		name    string
		frame   string
		wantErr error
	}{
		{
			name:    "is-error overrides success subtype",
			frame:   `{"type":"result","subtype":"success","is_error":true,"result":"IS-ERROR-SECRET"}`,
			wantErr: ErrClaudeResultFailed,
		},
		{
			name:    "maximum turns is classified",
			frame:   `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"MAX-TURNS-SECRET"}`,
			wantErr: ErrClaudeMaximumTurns,
		},
		{
			name:    "unknown subtype fails closed",
			frame:   `{"type":"result","subtype":"UNKNOWN-SUBTYPE-SECRET","is_error":true,"result":"UNKNOWN-RESULT-SECRET"}`,
			wantErr: ErrClaudeResultFailed,
		},
		{
			name:    "missing success subtype fails closed",
			frame:   `{"type":"result","is_error":false,"result":"MISSING-SUBTYPE-SECRET"}`,
			wantErr: ErrClaudeResultFailed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := writeFakeCLI(t, "fake-claude-failure.sh", `#!/bin/sh
IFS= read -r _
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"PARTIAL-CLAUDE-SECRET"}]}}'
printf '%s\n' '`+tc.frame+`'
sleep 300
`)
			result, err := ClaudeRunner{}.Run(context.Background(), Binding{
				Name:   "claude",
				Config: BindingConfig{Type: "claude", Binary: bin},
			}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Run error = %v, want %v", err, tc.wantErr)
			}
			if result != (Result{}) {
				t.Fatalf("failed result = %+v, want no partial result", result)
			}
			for _, secret := range []string{
				"PARTIAL-CLAUDE-SECRET",
				"IS-ERROR-SECRET",
				"MAX-TURNS-SECRET",
				"UNKNOWN-SUBTYPE-SECRET",
				"UNKNOWN-RESULT-SECRET",
				"MISSING-SUBTYPE-SECRET",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error surfaced provider payload %q: %v", secret, err)
				}
			}
		})
	}
}

func TestOpencodeRunnerConsumesEveryStepUntilNaturalCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-opencode-multistep.sh", `#!/bin/sh
printf '%s\n' '{"type":"text","sessionID":"ses_multi","part":{"type":"text","text":"first "}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses_multi","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":10,"output":2,"cache":{"read":3}},"cost":0.1}}'
printf '%s\n' '{"type":"text","sessionID":"ses_multi","part":{"type":"text","text":"second"}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses_multi","part":{"type":"step-finish","reason":"stop","tokens":{"input":20,"output":4,"cache":{"read":6}},"cost":0.2}}'
`)
	result, err := OpencodeRunner{}.Run(context.Background(), Binding{
		Name:   "opencode",
		Config: BindingConfig{Type: "opencode", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.AssistantOutput != "first second" {
		t.Fatalf("AssistantOutput = %q, want all text frames", result.AssistantOutput)
	}
	if result.Usage.InputTokens != 30 ||
		result.Usage.OutputTokens != 6 ||
		result.Usage.CachedInputTokens != 9 ||
		result.Usage.CostUSD < 0.299999 ||
		result.Usage.CostUSD > 0.300001 {
		t.Fatalf("aggregated Usage = %+v, want 30/6/9/0.3", result.Usage)
	}
}

func TestOpencodeRunnerAcceptsDocumentedFinalReasons(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	for _, reason := range []string{"stop", "length"} {
		t.Run(reason, func(t *testing.T) {
			bin := writeFakeCLI(t, "fake-opencode-final.sh", `#!/bin/sh
printf '%s\n' '{"type":"text","sessionID":"ses_final","part":{"type":"text","text":"done"}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses_final","part":{"type":"step-finish","reason":"`+reason+`","tokens":{"input":1,"output":1,"cache":{"read":0}},"cost":0}}'
`)
			if _, err := (OpencodeRunner{}).Run(context.Background(), Binding{
				Name:   "opencode",
				Config: BindingConfig{Type: "opencode", Binary: bin},
			}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"}); err != nil {
				t.Fatalf("Run final reason %q: %v", reason, err)
			}
		})
	}
}

func TestOpencodeRunnerRejectsErrorNonzeroAndNonterminalFinishWithoutPartialResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	tests := []struct {
		name    string
		body    string
		wantErr error
	}{
		{
			name: "error frame",
			body: `printf '%s\n' '{"type":"text","sessionID":"ses","part":{"type":"text","text":"PARTIAL-OPENCODE-SECRET"}}'
printf '%s\n' '{"type":"error","sessionID":"ses","error":{"name":"PROVIDER-ERROR-SECRET","data":{"message":"ERROR-MESSAGE-SECRET"}}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses","part":{"type":"step-finish","reason":"stop","tokens":{"input":1,"output":1,"cache":{"read":0}},"cost":0}}'`,
			wantErr: ErrOpencodeReportedError,
		},
		{
			name: "intermediate reason at exit",
			body: `printf '%s\n' '{"type":"text","sessionID":"ses","part":{"type":"text","text":"PARTIAL-OPENCODE-SECRET"}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses","part":{"type":"step-finish","reason":"tool-calls","tokens":{"input":1,"output":1,"cache":{"read":0}},"cost":0}}'`,
			wantErr: ErrOpencodeFinalReason,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := writeFakeCLI(t, "fake-opencode-failure.sh", "#!/bin/sh\n"+tc.body+"\n")
			result, err := OpencodeRunner{}.Run(context.Background(), Binding{
				Name:   "opencode",
				Config: BindingConfig{Type: "opencode", Binary: bin},
			}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Run error = %v, want %v", err, tc.wantErr)
			}
			if result != (Result{}) {
				t.Fatalf("failed result = %+v, want no partial result", result)
			}
			for _, secret := range []string{
				"PARTIAL-OPENCODE-SECRET",
				"PROVIDER-ERROR-SECRET",
				"ERROR-MESSAGE-SECRET",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error surfaced provider payload %q: %v", secret, err)
				}
			}
		})
	}

	bin := writeFakeCLI(t, "fake-opencode-nonzero.sh", `#!/bin/sh
printf '%s\n' '{"type":"text","sessionID":"ses","part":{"type":"text","text":"PARTIAL-NONZERO-SECRET"}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses","part":{"type":"step-finish","reason":"stop","tokens":{"input":1,"output":1,"cache":{"read":0}},"cost":0}}'
exit 23
`)
	result, err := OpencodeRunner{}.Run(context.Background(), Binding{
		Name:   "opencode",
		Config: BindingConfig{Type: "opencode", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "exited nonzero") {
		t.Fatalf("Run error = %v, want classified nonzero failure", err)
	}
	if result != (Result{}) || strings.Contains(err.Error(), "PARTIAL-NONZERO-SECRET") {
		t.Fatalf("nonzero result/error leaked partial provider result: result=%+v err=%v", result, err)
	}
}

func TestCodexFailureClassifierUsesWholeTokensAndPinnedPrecedence(t *testing.T) {
	falsePositives := []string{
		"modeling completed normally",
		"plugin loaded normally",
		"the transport was socketed",
		"request number 5030 finished",
		"validation400 passed",
		"the quotaic field is documentation",
	}
	for _, message := range falsePositives {
		if got := classifyCodexFailure(message); got != codexFailureGeneric {
			t.Errorf("classifyCodexFailure(%q) = %v, want generic", message, got)
		}
	}

	tests := []struct {
		message string
		want    codexFailureCategory
	}{
		{"401 service unavailable and rate limit 429", codexFailureAuthentication},
		{"rate_limit quota exhausted model forbidden", codexFailureRateLimit},
		{"quota exhausted due to context window", codexFailureQuota},
		{"context-window exceeded on service unavailable", codexFailureInvalidRequest},
		{"503 network timeout for model", codexFailureService},
		{"model timeout", codexFailureModelAccess},
		{"model access denied after network timeout", codexFailureModelAccess},
		{"network timeout for malformed request", codexFailureNetwork},
		{"invalid_request_error code 400", codexFailureInvalidRequest},
	}
	for _, tc := range tests {
		if got := classifyCodexFailure(tc.message); got != tc.want {
			t.Errorf("classifyCodexFailure(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}

func TestBoundedProviderResultAndEvidenceRejectWithoutPartialWrites(t *testing.T) {
	var result boundedResultBuffer
	if err := result.writeString(strings.Repeat("a", maxAuthoritativeResultBytes)); err != nil {
		t.Fatalf("write exact result ceiling: %v", err)
	}
	if err := result.writeString("RESULT-OVERFLOW-SECRET"); !errors.Is(err, ErrAuthoritativeResultTooLarge) {
		t.Fatalf("result overflow error = %v, want ErrAuthoritativeResultTooLarge", err)
	}
	if got := result.String(); len(got) != maxAuthoritativeResultBytes ||
		strings.Contains(got, "RESULT-OVERFLOW-SECRET") {
		t.Fatalf("result buffer accepted a partial overflowing write")
	}

	var combined boundedResultBuffer
	if err := combined.writeStringReserved(
		strings.Repeat("b", maxAuthoritativeResultBytes-3),
		3,
	); err != nil {
		t.Fatalf("write result with session reservation: %v", err)
	}
	if err := combined.writeStringReserved("x", 3); !errors.Is(err, ErrAuthoritativeResultTooLarge) {
		t.Fatalf("combined result/session overflow error = %v, want ErrAuthoritativeResultTooLarge", err)
	}

	path, cleanup, err := newResultFile("bounded-evidence-test-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	evidence, err := newBoundedEvidenceFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.writeFrame(make([]byte, maxStructuredEvidenceBytes)); err != nil {
		t.Fatalf("write exact evidence ceiling: %v", err)
	}
	if err := evidence.writeFrame([]byte("EVIDENCE-OVERFLOW-SECRET")); !errors.Is(err, ErrStructuredEvidenceTooLarge) {
		t.Fatalf("evidence overflow error = %v, want ErrStructuredEvidenceTooLarge", err)
	}
	if err := evidence.close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentBackedProvidersEnforceAuthoritativeByteCeilings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	t.Run("codex result file", func(t *testing.T) {
		bin := writeFakeCLI(t, "fake-codex-result-overflow.sh", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; shift 2; else shift; fi
done
python3 - "$out" <<'PY'
import sys
with open(sys.argv[1], "wb") as f:
    f.write(b"x" * ((16 << 20) + 1))
    f.write(b"CODEX-OVERFLOW-SECRET")
PY
printf '%s\n' '{"type":"turn.completed"}'
`)
		result, err := CodexRunner{}.Run(context.Background(), Binding{
			Name:   "codex",
			Config: BindingConfig{Type: "codex", Binary: bin},
		}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
		if !errors.Is(err, ErrAuthoritativeResultTooLarge) {
			t.Fatalf("Run error = %v, want ErrAuthoritativeResultTooLarge", err)
		}
		if result != (Result{}) || strings.Contains(err.Error(), "CODEX-OVERFLOW-SECRET") {
			t.Fatalf("overflow surfaced partial result/payload: result=%+v err=%v", result, err)
		}
	})

	t.Run("claude accumulated stream", func(t *testing.T) {
		bin := writeFakeCLI(t, "fake-claude-result-overflow.sh", `#!/bin/sh
IFS= read -r _
python3 <<'PY'
import json
for _ in range(20):
    print(json.dumps({"type":"assistant","message":{"content":[{"type":"text","text":"x"*900000}]}}))
print(json.dumps({"type":"result","subtype":"success","is_error":False}))
PY
sleep 300
`)
		result, err := ClaudeRunner{}.Run(context.Background(), Binding{
			Name:   "claude",
			Config: BindingConfig{Type: "claude", Binary: bin},
		}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
		if !errors.Is(err, ErrStructuredEvidenceTooLarge) {
			t.Fatalf("Run error = %v, want ErrStructuredEvidenceTooLarge", err)
		}
		if result != (Result{}) {
			t.Fatalf("overflow result = %+v, want no partial result", result)
		}
	})

	t.Run("opencode evidence tee", func(t *testing.T) {
		bin := writeFakeCLI(t, "fake-opencode-evidence-overflow.sh", `#!/bin/sh
python3 <<'PY'
import json
for _ in range(20):
    print(json.dumps({"type":"system","sessionID":"ses","padding":"x"*900000}))
PY
sleep 300
`)
		result, err := OpencodeRunner{}.Run(context.Background(), Binding{
			Name:   "opencode",
			Config: BindingConfig{Type: "opencode", Binary: bin},
		}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
		if !errors.Is(err, ErrStructuredEvidenceTooLarge) {
			t.Fatalf("Run error = %v, want ErrStructuredEvidenceTooLarge", err)
		}
		if result != (Result{}) {
			t.Fatalf("overflow result = %+v, want no partial result", result)
		}
	})
}

func TestSuperviseAgentRechecksContextAfterCallbackAndConvergence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	t.Run("callback", func(t *testing.T) {
		bin := writeFakeCLI(t, "fake-cancel-in-callback.sh", "#!/bin/sh\nprintf 'line\\n'\nsleep 300\n")
		ctx, cancel := context.WithCancel(context.Background())
		a, err := agent.Start(ctx, agent.Options{Command: bin})
		if err != nil {
			t.Fatal(err)
		}
		err = superviseAgent(ctx, a, agent.WatchdogConfig{}, func([]byte) bool {
			cancel()
			return true
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("superviseAgent = %v, want context.Canceled", err)
		}
	})

	t.Run("terminate convergence", func(t *testing.T) {
		bin := writeFakeCLI(t, "fake-cancel-in-terminate.sh", "#!/bin/sh\nprintf 'line\\n'\nsleep 300\n")
		ctx, cancel := context.WithCancel(context.Background())
		a, err := agent.Start(ctx, agent.Options{Command: bin})
		if err != nil {
			t.Fatal(err)
		}
		convergence := defaultAgentConvergence()
		actual := convergence.terminateAndWait
		convergence.terminateAndWait = func(a *agent.Agent) error {
			err := actual(a)
			cancel()
			return err
		}
		err = superviseAgentWithConvergence(
			ctx,
			a,
			agent.WatchdogConfig{},
			func([]byte) bool { return true },
			convergence,
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("superviseAgent = %v, want context.Canceled", err)
		}
	})

	t.Run("natural convergence", func(t *testing.T) {
		bin := writeFakeCLI(t, "fake-cancel-in-wait.sh", "#!/bin/sh\nexit 0\n")
		ctx, cancel := context.WithCancel(context.Background())
		a, err := agent.Start(ctx, agent.Options{Command: bin})
		if err != nil {
			t.Fatal(err)
		}
		convergence := defaultAgentConvergence()
		actual := convergence.wait
		convergence.wait = func(a *agent.Agent) error {
			err := actual(a)
			cancel()
			return err
		}
		err = superviseAgentWithConvergence(
			ctx,
			a,
			agent.WatchdogConfig{},
			nil,
			convergence,
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("superviseAgent = %v, want context.Canceled", err)
		}
	})
}
