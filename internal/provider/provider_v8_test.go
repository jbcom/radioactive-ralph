package provider

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestCodexDiagnosticFrameBoundExcludesAgentDelimiter(t *testing.T) {
	tests := []struct {
		name           string
		frameBytes     int
		wantDiagnostic string
		wantInspected  int
	}{
		{
			name:           "boundary minus one",
			frameBytes:     maxCodexDiagnosticFrameBytes - 1,
			wantDiagnostic: codexFailureQuota.String(),
			wantInspected:  maxCodexDiagnosticFrameBytes - 1,
		},
		{
			name:           "boundary",
			frameBytes:     maxCodexDiagnosticFrameBytes,
			wantDiagnostic: codexFailureQuota.String(),
			wantInspected:  maxCodexDiagnosticFrameBytes,
		},
		{
			name:           "boundary plus one",
			frameBytes:     maxCodexDiagnosticFrameBytes + 1,
			wantDiagnostic: codexFailureGeneric.String(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame := codexTurnFailedFrameOfSize(t, tc.frameBytes)
			line := append(append([]byte(nil), frame...), '\n')
			var diagnostics codexDiagnosticCollector
			diagnostics.consume(line)

			if !errors.Is(diagnostics.failure(), ErrCodexTurnFailed) {
				t.Fatalf("failure = %v, want ErrCodexTurnFailed", diagnostics.failure())
			}
			if got := diagnostics.String(); got != tc.wantDiagnostic {
				t.Fatalf("diagnostic = %q, want %q", got, tc.wantDiagnostic)
			}
			if diagnostics.bytesInspected != tc.wantInspected {
				t.Fatalf("bytesInspected = %d, want %d", diagnostics.bytesInspected, tc.wantInspected)
			}
			if strings.Contains(diagnostics.String(), "BOUNDARY-PAYLOAD-SECRET") {
				t.Fatal("diagnostic surfaced provider payload")
			}
		})
	}
}

func TestCodexRunnerExactDiagnosticBoundaryTurnFailedIsAuthoritative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-exact-diagnostic-boundary.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-BOUNDARY-SUCCESS' > "$out"
python3 - <<'PY'
import sys
size = 65536
prefix = '{"type":"turn.failed","error":{"message":"quota exhausted BOUNDARY-RUNNER-SECRET'
suffix = '"}}'
sys.stdout.write(prefix + ("x" * (size - len(prefix) - len(suffix))) + suffix + "\n")
PY
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexTurnFailed) {
		t.Fatalf("Run error = %v, want ErrCodexTurnFailed", err)
	}
	if !strings.Contains(err.Error(), codexFailureQuota.String()) {
		t.Fatalf("Run error = %v, want static quota category", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged result", result)
	}
	for _, secret := range []string{"FORGED-BOUNDARY-SUCCESS", "BOUNDARY-RUNNER-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestCodexRunnerDuplicateTopLevelTypeFailsClosedInEitherOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "nonterminal then failure",
			frame: `{"type":"item.completed","padding":"DUPLICATE-FIRST-SECRET","type":"turn.failed","error":{"message":"DUPLICATE-FAILURE-SECRET"}}`,
		},
		{
			name:  "failure then nonterminal",
			frame: `{"type":"turn.failed","error":{"message":"DUPLICATE-FAILURE-SECRET"},"type":"item.completed","padding":"DUPLICATE-LAST-SECRET"}`,
		},
		{
			name:  "semantic escapes",
			frame: `{"t\u0079pe":"item.completed","\u0074ype":"turn.failed","error":{"message":"DUPLICATE-ESCAPE-SECRET"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bin := fakeCodexCLI(t, "fake-codex-duplicate-type.sh", "FORGED-DUPLICATE-SUCCESS", tc.frame, 0)
			result, err := CodexRunner{}.Run(context.Background(), Binding{
				Name:            "codex",
				BinaryFromLocal: true,
				Config:          BindingConfig{Type: "codex", Binary: bin},
			}, Request{WorkingDir: t.TempDir()})
			if !errors.Is(err, ErrCodexOversizeSchema) {
				t.Fatalf("Run error = %v, want ErrCodexOversizeSchema", err)
			}
			if result != (Result{}) {
				t.Fatalf("failed result = %+v, want no forged result", result)
			}
			for _, secret := range []string{
				"FORGED-DUPLICATE-SUCCESS",
				"DUPLICATE-FIRST-SECRET",
				"DUPLICATE-FAILURE-SECRET",
				"DUPLICATE-LAST-SECRET",
				"DUPLICATE-ESCAPE-SECRET",
			} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error surfaced provider payload %q: %v", secret, err)
				}
			}
		})
	}
}

func TestCodexTypeDiscriminatorIsCaseSensitive(t *testing.T) {
	tests := []string{
		`{"TYPE":"turn.failed","error":{"message":"CASE-KEY-SECRET"}}`,
		`{"type":"TURN.FAILED","error":{"message":"CASE-VALUE-SECRET"}}`,
	}
	for _, frame := range tests {
		var diagnostics codexDiagnosticCollector
		diagnostics.consume([]byte(frame + "\n"))
		if diagnostics.failure() != nil {
			t.Fatalf("frame %q produced false terminal failure %v", frame, diagnostics.failure())
		}
	}
}

func TestCodexRunnerTypeFirstObjectBeyondInspectionBoundFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-bound-plus-one-command.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-BOUND-PLUS-ONE-SUCCESS' > "$out"
python3 - <<'PY'
import sys
size = `+strconv.Itoa(codexRetainedJSONLLineBytes+1)+`
prefix = '{"type":"item.completed","padding":"'
suffix = '"}'
sys.stdout.write(prefix + ("x" * (size - len(prefix) - len(suffix))) + suffix + "\n")
PY
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexOversizeSchema) {
		t.Fatalf("Run error = %v, want ErrCodexOversizeSchema", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged result", result)
	}
	if strings.Contains(err.Error(), "FORGED-BOUND-PLUS-ONE-SUCCESS") {
		t.Fatalf("error surfaced provider payload: %v", err)
	}
}

func TestCodexRunnerDiscardedEmptyObjectPrefixFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-empty-object-prefix.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-EMPTY-OBJECT-SUCCESS' > "$out"
python3 - <<'PY'
import sys
sys.stdout.write("{}EMPTY-OBJECT-TAIL-SECRET" + ("x" * `+strconv.Itoa(codexRetainedJSONLLineBytes)+`) + "\n")
PY
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexOversizeSchema) {
		t.Fatalf("Run error = %v, want ErrCodexOversizeSchema", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged result", result)
	}
	for _, secret := range []string{"FORGED-EMPTY-OBJECT-SUCCESS", "EMPTY-OBJECT-TAIL-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestCodexRunnerDiscardedLeadingWhitespaceFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	for _, leadingBytes := range []int{4095, 4096, 4097} {
		t.Run(strconv.Itoa(leadingBytes), func(t *testing.T) {
			bin := writeFakeCLI(t, "fake-codex-leading-whitespace.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-WHITESPACE-SUCCESS' > "$out"
python3 - <<'PY'
import sys
leading = `+strconv.Itoa(leadingBytes)+`
prefix = (" " * leading) + '{"type":"turn.failed","error":{"message":"WHITESPACE-FAILURE-SECRET'
suffix = '"},"padding":"' + ("x" * `+strconv.Itoa(codexRetainedJSONLLineBytes)+`) + '"}'
sys.stdout.write(prefix + suffix + "\n")
PY
`)
			result, err := CodexRunner{}.Run(context.Background(), Binding{
				Name:            "codex",
				BinaryFromLocal: true,
				Config:          BindingConfig{Type: "codex", Binary: bin},
			}, Request{WorkingDir: t.TempDir()})
			if !errors.Is(err, ErrCodexOversizeSchema) {
				t.Fatalf("Run error = %v, want ErrCodexOversizeSchema", err)
			}
			if result != (Result{}) {
				t.Fatalf("failed result = %+v, want no forged result", result)
			}
			for _, secret := range []string{"FORGED-WHITESPACE-SUCCESS", "WHITESPACE-FAILURE-SECRET"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error surfaced provider payload %q: %v", secret, err)
				}
			}
		})
	}
}

func codexTurnFailedFrameOfSize(t *testing.T, size int) []byte {
	t.Helper()
	const (
		prefix = `{"type":"turn.failed","error":{"message":"quota exhausted BOUNDARY-PAYLOAD-SECRET`
		suffix = `"}}`
	)
	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		t.Fatalf("requested frame size %d is smaller than fixed envelope %d", size, len(prefix)+len(suffix))
	}
	frame := []byte(prefix + strings.Repeat("x", padding) + suffix)
	if len(frame) != size {
		t.Fatalf("constructed frame = %d bytes, want %d", len(frame), size)
	}
	return frame
}
