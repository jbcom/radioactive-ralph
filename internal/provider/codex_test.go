package provider

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeCodexCLI returns a shell script standing in for the `codex` binary: it
// scans its args for `--output-last-message <path>`, writes msg there, prints
// stdout, and exits with code. This lets the tests exercise CodexRunner's
// exit-code and safe-diagnostic handling without a real codex install.
func fakeCodexCLI(t *testing.T, name, msg, stdout string, code int) string {
	t.Helper()
	return writeFakeCLI(t, name, `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message)
      out="$2"
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
[ -n "$out" ] && printf '%s' '`+msg+`' > "$out"
printf '%s\n' '`+stdout+`'
exit `+strconv.Itoa(code)+`
`)
}

// TestCodexRunnerFailsOnNonzeroExit is the regression guard for the
// laundered-failure bug: a failed CLI run that wrote a partial message to
// --output-last-message before exiting nonzero must FAIL the turn, not be
// reported as a successful (and zero-cost) result. Neither the partial result
// nor an arbitrary terminal tail is a safe diagnostic source.
func TestCodexRunnerFailsOnNonzeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := fakeCodexCLI(t, "fake-codex-fail.sh", "partial diagnostic output", "some stderr-ish line", 1)
	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true, // fake binary path is local.toml-authorized
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err == nil {
		t.Fatal("codex exited nonzero after writing a partial message; want the turn to FAIL, got success")
	}
	if !strings.Contains(err.Error(), "exited nonzero") {
		t.Fatalf("err = %v, want it to mention a nonzero exit", err)
	}
	if !strings.Contains(err.Error(), codexFailureGeneric.String()) {
		t.Fatalf("err = %v, want fixed generic category", err)
	}
	for _, excluded := range []string{"partial diagnostic output", "some stderr-ish line"} {
		if strings.Contains(err.Error(), excluded) {
			t.Fatalf("err = %q surfaced unsafe failure text %q", err, excluded)
		}
	}
}

func TestCodexRunnerSurfacesOnlySafeStructuredFailureDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	const (
		partialSecret   = "PARTIAL_LAST_MESSAGE_SECRET"
		assistantSecret = "ASSISTANT_EVENT_SECRET"
		reasoningSecret = "REASONING_EVENT_SECRET"
		toolSecret      = "TOOL_EVENT_SECRET"
		promptSecret    = "PROMPT_EVENT_SECRET"
		rawTailSecret   = "RAW_TAIL_SECRET"
		tokenSecret     = "sk-proj-supersecret1234"
		passwordSecret  = "hunter2"
		urlSecret       = "user:pass"
	)
	stdout := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-safe"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"` + assistantSecret + `"}}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"` + reasoningSecret + `"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"` + toolSecret + `","aggregated_output":"` + toolSecret + `"}}`,
		`{"type":"item.completed","item":{"type":"user_message","text":"` + promptSecret + `"}}`,
		`{"type":"error","message":"permission denied: Authorization: Bearer ` + tokenSecret + `; password=\"` + passwordSecret + `\"; https://` + urlSecret + `@example.test/private"}`,
		`{"type":"error","message":"permission denied: Authorization: Bearer ` + tokenSecret + `; password=\"` + passwordSecret + `\"; https://` + urlSecret + `@example.test/private"}`,
		`{"type":"turn.failed","message":"WRONG_TOP_LEVEL_FIELD","error":{"message":"quota exhausted\u001b"}}`,
		`{"type":"assistant","message":"WRONG_EVENT_TYPE"}`,
		rawTailSecret,
	}, "\n")
	bin := fakeCodexCLI(t, "fake-codex-structured-fail.sh", partialSecret, stdout, 17)

	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "safe task"})
	if err == nil {
		t.Fatal("CodexRunner.Run succeeded, want structured nonzero failure")
	}
	got := err.Error()
	for _, included := range []string{
		codexFailureModelAccess.String(),
		codexFailureQuota.String(),
	} {
		if !strings.Contains(got, included) {
			t.Errorf("error = %q, want fixed diagnostic category %q", got, included)
		}
	}
	if count := strings.Count(got, codexFailureModelAccess.String()); count != 1 {
		t.Errorf("duplicate diagnostic count = %d, want 1; error=%q", count, got)
	}
	for _, excluded := range []string{
		partialSecret,
		assistantSecret,
		reasoningSecret,
		toolSecret,
		promptSecret,
		rawTailSecret,
		tokenSecret,
		passwordSecret,
		urlSecret,
		"WRONG_TOP_LEVEL_FIELD",
		"WRONG_EVENT_TYPE",
	} {
		if strings.Contains(got, excluded) {
			t.Errorf("error surfaced excluded content %q: %q", excluded, got)
		}
	}
}

func TestCodexDiagnosticCollectorUsesClosedBoundedVocabulary(t *testing.T) {
	var diagnostics codexDiagnosticCollector
	for range 100 {
		for _, frame := range []string{
			`{"type":"error","message":"authentication failed SECRET-A"}`,
			`{"type":"error","message":"model unavailable SECRET-B"}`,
			`{"type":"error","message":"quota exhausted SECRET-C"}`,
			`{"type":"error","message":"rate limit exceeded SECRET-D"}`,
			`{"type":"error","message":"network timeout SECRET-E"}`,
			`{"type":"error","message":"service unavailable SECRET-F"}`,
			`{"type":"error","message":"invalid request SECRET-G"}`,
			`{"type":"error","message":"unclassified SECRET-H"}`,
		} {
			diagnostics.consume([]byte(frame))
		}
	}

	got := diagnostics.String()
	if len(got) > maxCodexDiagnosticBytes {
		t.Fatalf("diagnostic is %d bytes, want <= %d", len(got), maxCodexDiagnosticBytes)
	}
	categories := []codexFailureCategory{
		codexFailureAuthentication,
		codexFailureModelAccess,
		codexFailureQuota,
		codexFailureRateLimit,
		codexFailureNetwork,
		codexFailureService,
		codexFailureInvalidRequest,
		codexFailureGeneric,
	}
	wantParts := make([]string, 0, len(categories))
	for _, category := range categories {
		wantParts = append(wantParts, category.String())
		if count := strings.Count(got, category.String()); count != 1 {
			t.Errorf("category %q count = %d, want 1; diagnostic=%q", category, count, got)
		}
	}
	if want := strings.Join(wantParts, "; "); got != want {
		t.Errorf("diagnostic = %q, want closed vocabulary %q", got, want)
	}
	for _, excluded := range []string{
		"SECRET-A", "SECRET-B", "SECRET-C", "SECRET-D",
		"SECRET-E", "SECRET-F", "SECRET-G", "SECRET-H",
	} {
		if strings.Contains(got, excluded) {
			t.Errorf("diagnostic surfaced provider text %q: %q", excluded, got)
		}
	}
	if !diagnostics.full || len(diagnostics.categories) != int(codexFailureCategoryCount) {
		t.Fatalf("collector did not become information-complete: full=%v categories=%d", diagnostics.full, len(diagnostics.categories))
	}
	before := diagnostics.String()
	diagnostics.consume([]byte(`{"type":"error","message":"` + strings.Repeat("HOSTILE-AFTER-FULL-", 1<<16) + `"}`))
	if got := diagnostics.String(); got != before {
		t.Fatalf("information-complete collector changed after hostile frame: before=%q after=%q", before, got)
	}

	var failClosed codexDiagnosticCollector
	failClosed.add(codexFailureCategory(255))
	if got := failClosed.String(); got != codexFailureGeneric.String() {
		t.Fatalf("unknown category rendered as %q, want fail-closed %q", got, codexFailureGeneric)
	}
}

func TestCodexDiagnosticCollectorWorkBudgetsFailClosed(t *testing.T) {
	assertGenericExhausted := func(t *testing.T, diagnostics *codexDiagnosticCollector, secret string) {
		t.Helper()
		if !diagnostics.full || !diagnostics.exhausted {
			t.Fatalf("collector did not terminate on budget exhaustion: full=%v exhausted=%v", diagnostics.full, diagnostics.exhausted)
		}
		if got := diagnostics.String(); got != codexFailureGeneric.String() {
			t.Fatalf("exhausted diagnostic = %q, want %q", got, codexFailureGeneric)
		}
		if strings.Contains(diagnostics.String(), secret) {
			t.Fatalf("exhausted diagnostic leaked %q", secret)
		}
	}

	t.Run("per frame", func(t *testing.T) {
		const secret = "PER-FRAME-BUDGET-SECRET"
		var diagnostics codexDiagnosticCollector
		diagnostics.consume([]byte(`{"type":"error","message":"authentication failed"}`))
		diagnostics.consume([]byte(`{"type":"error","message":"` +
			secret + strings.Repeat("x", maxCodexDiagnosticFrameBytes) + `"}`))
		assertGenericExhausted(t, &diagnostics, secret)
		if diagnostics.framesInspected != 1 {
			t.Fatalf("framesInspected = %d, want only the pre-budget frame", diagnostics.framesInspected)
		}
	})

	t.Run("frame count", func(t *testing.T) {
		const secret = "FRAME-COUNT-BUDGET-SECRET"
		var diagnostics codexDiagnosticCollector
		frame := []byte(`{"type":"thread.started","marker":"` + secret + `"}`)
		for range maxCodexDiagnosticFrames + 1 {
			diagnostics.consume(frame)
		}
		assertGenericExhausted(t, &diagnostics, secret)
		if diagnostics.framesInspected != maxCodexDiagnosticFrames {
			t.Fatalf("framesInspected = %d, want %d", diagnostics.framesInspected, maxCodexDiagnosticFrames)
		}
	})

	t.Run("total bytes", func(t *testing.T) {
		const secret = "TOTAL-BYTE-BUDGET-SECRET"
		var diagnostics codexDiagnosticCollector
		frame := []byte(`{"type":"thread.started","padding":"` +
			secret + strings.Repeat("x", 40<<10) + `"}`)
		for attempt := 0; attempt <= maxCodexDiagnosticFrames && !diagnostics.full; attempt++ {
			diagnostics.consume(frame)
		}
		if !diagnostics.full {
			t.Fatal("diagnostic byte budget did not converge within the frame bound")
		}
		assertGenericExhausted(t, &diagnostics, secret)
		if diagnostics.bytesInspected > maxCodexDiagnosticInspectedBytes {
			t.Fatalf("bytesInspected = %d, want <= %d", diagnostics.bytesInspected, maxCodexDiagnosticInspectedBytes)
		}
	})
}

func TestClassifyCodexFailureReturnsOnlyStaticCategories(t *testing.T) {
	for name, tc := range map[string]struct {
		message string
		want    codexFailureCategory
	}{
		"authentication": {message: "401 authentication failed SECRET-A", want: codexFailureAuthentication},
		"model":          {message: "model SECRET-B unavailable", want: codexFailureModelAccess},
		"quota":          {message: "quota exhausted SECRET-C", want: codexFailureQuota},
		"rate limit":     {message: "429 too many requests SECRET-D", want: codexFailureRateLimit},
		"network":        {message: "network timeout to SECRET-E", want: codexFailureNetwork},
		"service":        {message: "503 service unavailable SECRET-F", want: codexFailureService},
		"invalid request": {
			message: "400 invalid request SECRET-G",
			want:    codexFailureInvalidRequest,
		},
		"unknown": {message: "mysterious failure SECRET-H", want: codexFailureGeneric},
	} {
		t.Run(name, func(t *testing.T) {
			if got := classifyCodexFailure(tc.message); got != tc.want {
				t.Fatalf("classifyCodexFailure(%q) = %q, want %q", tc.message, got, tc.want)
			}
			if strings.Contains(tc.want.String(), "SECRET-") {
				t.Fatalf("static category retained provider content: %q", tc.want)
			}
		})
	}
}

func TestCodexRunnerAdversarialErrorMessagesCannotCrossBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	const (
		promptFragment = "PROMPT-FRAGMENT-DO-NOT-LEAK"
		schemaFragment = "REFORMATTED-SCHEMA-DO-NOT-LEAK"
		pemFragment    = "UNCLOSED-PEM-DO-NOT-LEAK"
		credential     = "CREDENTIAL-VALUE-DO-NOT-LEAK"
		apiKeyValue    = "API-KEY-VALUE-DO-NOT-LEAK"
		unknownValue   = "UNKNOWN-VALUE-DO-NOT-LEAK"
		modelID        = "MODEL-ID-DO-NOT-LEAK"
		urlValue       = "https://secret.example.test/private"
		statusValue    = "599-DO-NOT-LEAK"
	)
	stdout := strings.Join([]string{
		`{"type":"error","message":"invalid request echoed partial ` + promptFragment + `"}`,
		`{"type":"error","message":"bad request after schema normalization ` + schemaFragment + `"}`,
		`{"type":"error","message":"authentication rejected -----BEGIN PRIVATE KEY----- ` + pemFragment + `"}`,
		`{"type":"error","message":"credential was ` + credential + `"}`,
		`{"type":"error","message":"OPENAI_API_KEY was rejected with value ` + apiKeyValue + `"}`,
		`{"type":"error","message":"mysterious failure ` + unknownValue + `"}`,
		`{"type":"turn.failed","error":{"message":"model ` + modelID + ` unavailable at ` + urlValue + ` status ` + statusValue + `"}}`,
	}, "\n")
	bin := fakeCodexCLI(t, "fake-codex-adversarial-errors.sh", "PARTIAL-RESULT-DO-NOT-LEAK", stdout, 1)

	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "SYSTEM " + promptFragment,
		UserPrompt:   "USER " + promptFragment,
		OutputSchema: `{"description":"` + schemaFragment + `"}`,
	})
	if err == nil {
		t.Fatal("CodexRunner.Run succeeded, want nonzero failure")
	}
	got := err.Error()
	for _, category := range []codexFailureCategory{
		codexFailureInvalidRequest,
		codexFailureAuthentication,
		codexFailureGeneric,
		codexFailureModelAccess,
	} {
		if !strings.Contains(got, category.String()) {
			t.Errorf("error = %q, want fixed category %q", got, category)
		}
	}
	for _, excluded := range []string{
		promptFragment,
		schemaFragment,
		pemFragment,
		credential,
		apiKeyValue,
		unknownValue,
		modelID,
		urlValue,
		statusValue,
		"PARTIAL-RESULT-DO-NOT-LEAK",
		"BEGIN PRIVATE KEY",
		"OPENAI_API_KEY",
	} {
		if strings.Contains(got, excluded) {
			t.Errorf("error surfaced adversarial provider content %q: %q", excluded, got)
		}
	}
}

func TestCodexDiagnosticCollectorIgnoresEveryOtherEventShape(t *testing.T) {
	var diagnostics codexDiagnosticCollector
	for _, frame := range []string{
		`not json`,
		`{"type":"thread.started","message":"THREAD_SECRET"}`,
		`{"type":"turn.started","message":"TURN_SECRET"}`,
		`{"type":"item.started","message":"ITEM_SECRET"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"ASSISTANT_SECRET"}}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"REASONING_SECRET"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"ARGV_SECRET","aggregated_output":"TOOL_SECRET"}}`,
		`{"type":"turn.completed","message":"SUCCESS_SECRET"}`,
		`{"type":"error","error":{"message":"WRONG_NESTED_ERROR_FIELD"}}`,
		`{"type":"turn.failed","message":"WRONG_TOP_LEVEL_FAILURE_FIELD"}`,
	} {
		diagnostics.consume([]byte(frame))
	}
	if len(diagnostics.categories) != 0 {
		t.Fatalf("collector retained non-diagnostic events: %#v", diagnostics.categories)
	}
	if got := diagnostics.String(); got != codexFailureGeneric.String() {
		t.Fatalf("empty collector fallback = %q, want %q", got, codexFailureGeneric)
	}
}

// TestCodexRunnerSucceedsOnCleanExit confirms the exit gate does not regress the
// happy path: a codex that writes its message and exits 0 yields the message.
func TestCodexRunnerSucceedsOnCleanExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := fakeCodexCLI(t, "fake-codex-ok.sh", `{"outcome":"done","summary":"codex ok","evidence":["c"]}`, "done", 0)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("clean codex exit should succeed: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "codex ok") {
		t.Fatalf("AssistantOutput = %q, want the written message", result.AssistantOutput)
	}
}

func TestCodexRunnerSuccessDoesNotMixFailureDiagnosticsIntoResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := fakeCodexCLI(
		t,
		"fake-codex-success-with-retry.sh",
		`{"outcome":"done","summary":"clean success","evidence":["result file only"]}`,
		`{"type":"error","message":"transient retry diagnostic"}`,
		0,
	)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("CodexRunner.Run: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "clean success") {
		t.Fatalf("AssistantOutput = %q, want last-message success", result.AssistantOutput)
	}
	if strings.Contains(result.AssistantOutput, "transient retry diagnostic") {
		t.Fatalf("AssistantOutput mixed terminal diagnostics into success: %q", result.AssistantOutput)
	}
}

func TestCodexRunnerUsesExactSafeStructuredArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	capturePath := filepath.Join(t.TempDir(), "codex-args.b64")
	t.Setenv("RALPH_TEST_CODEX_ARGS", capturePath)
	bin := writeFakeCLI(t, "fake-codex-args.sh", `#!/bin/sh
out=""
previous=""
: > "$RALPH_TEST_CODEX_ARGS"
for arg in "$@"; do
  printf '%s' "$arg" | base64 | tr -d '\n' >> "$RALPH_TEST_CODEX_ARGS"
  printf '\n' >> "$RALPH_TEST_CODEX_ARGS"
  if [ "$previous" = "--output-last-message" ]; then
    out="$arg"
  fi
  previous="$arg"
done
printf '%s' '{"outcome":"done","summary":"args captured","evidence":[]}' > "$out"
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0}}'
`)
	workDir := t.TempDir()
	req := Request{
		WorkingDir:   workDir,
		SystemPrompt: "system",
		UserPrompt:   "user",
		OutputSchema: `{"type":"object"}`,
		Model:        ModelSonnet,
		Effort:       "high",
	}
	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config: BindingConfig{
			Type:        "codex",
			Binary:      bin,
			Args:        []string{"--ephemeral"},
			SonnetModel: "gpt-test-model",
		},
	}, req)
	if err != nil {
		t.Fatalf("CodexRunner.Run: %v", err)
	}
	raw, err := os.ReadFile(capturePath) //nolint:gosec // test-owned capture path
	if err != nil {
		t.Fatalf("read captured args: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	args := make([]string, 0, len(lines))
	for _, line := range lines {
		decoded, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			t.Fatalf("decode captured arg %q: %v", line, err)
		}
		args = append(args, string(decoded))
	}
	// 18, not 16: the resolved reasoning effort now reaches the process as a
	// `-c model_reasoning_effort=...` config override. It was previously
	// recorded on Result.Invocation and never passed, so provenance asserted an
	// effort codex had not run.
	if len(args) != 18 {
		t.Fatalf("args = %#v, want exactly 18 arguments", args)
	}
	wantFixed := map[int]string{
		0:  "exec",
		1:  "--json",
		2:  "--color",
		3:  "never",
		4:  "--skip-git-repo-check",
		5:  "--dangerously-bypass-approvals-and-sandbox",
		6:  "-C",
		7:  workDir,
		8:  "--output-last-message",
		10: "-m",
		11: "gpt-test-model",
		12: "-c",
		13: `model_reasoning_effort="high"`,
		14: "--output-schema",
		16: "--ephemeral",
		17: combinePrompt(req),
	}
	for index, want := range wantFixed {
		if got := args[index]; got != want {
			t.Errorf("args[%d] = %q, want %q; args=%#v", index, got, want, args)
		}
	}
	if filepath.Base(args[9]) != "last-message.txt" {
		t.Errorf("last-message path = %q, want last-message.txt", args[9])
	}
	if filepath.Base(args[15]) != "schema.json" {
		t.Errorf("schema path = %q, want schema.json", args[15])
	}
	if filepath.Dir(args[9]) != filepath.Dir(args[15]) {
		t.Errorf("last-message and schema files do not share an isolated temp dir: %q vs %q", args[9], args[13])
	}
}

// TestCodexArgsPassTheResolvedEffort is CodeRabbit's P1 on #234, and it caught
// a gap against this project's own decision record: the record said to TAKE
// `-c model_reasoning_effort=%q` in this PR, and the plumbing landed on
// Result.Invocation without ever reaching the process.
//
// The consequence is worse than a missing flag. ResolveInvocation records the
// effort and StrictBinding validates it, so Result.Invocation asserted an
// effort that codex never ran — provenance that is confidently wrong.
func TestCodexArgsPassTheResolvedEffort(t *testing.T) {
	binding := Binding{
		Name:   "codex-pool",
		Config: BindingConfig{Type: "codex", Binary: "codex", HighEffort: "high"},
	}
	inv, err := ResolveInvocation(binding, Request{Model: ModelSonnet, Effort: "high"})
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	args := codexArgs(binding, Request{WorkingDir: "/tmp/x"}, inv, "/tmp/out.txt", "")

	idx := -1
	for i, a := range args {
		if a == "-c" {
			idx = i
		}
	}
	if idx < 0 || idx+1 >= len(args) {
		t.Fatalf("args = %v, want a -c config override carrying the effort", args)
	}
	if want := `model_reasoning_effort="high"`; args[idx+1] != want {
		t.Fatalf("config override = %q, want %q", args[idx+1], want)
	}
}

// TestCodexArgsOmitTheOverrideForDefaultEffort keeps Ralph out of the way when
// the request names no specific effort. Passing "default" through would REPLACE
// the operator's own config.toml value with a literal that is not a valid
// effort — worse than sending nothing.
func TestCodexArgsOmitTheOverrideForDefaultEffort(t *testing.T) {
	binding := Binding{Name: "codex-pool", Config: BindingConfig{Type: "codex", Binary: "codex"}}
	for _, effort := range []string{"", "default"} {
		inv, err := ResolveInvocation(binding, Request{Model: ModelSonnet, Effort: effort})
		if err != nil {
			t.Fatalf("ResolveInvocation(%q): %v", effort, err)
		}
		args := codexArgs(binding, Request{WorkingDir: "/tmp/x"}, inv, "/tmp/out.txt", "")
		for i, a := range args {
			if a == "-c" {
				t.Errorf("effort %q produced a config override %q; the operator's "+
					"configured lane must be left alone", effort, args[i+1])
			}
		}
	}
}
