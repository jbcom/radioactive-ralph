package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	declarativePlainStdout       = "plain-stdout"
	declarativeLastMessageFile   = "last-message-file"
	declarativeStreamJSON        = "stream-json"
	declarativeStreamJSONLineMax = 16 << 20
)

// ErrStreamJSONLineTooLong reports that a stream-json provider emitted a
// single frame larger than declarativeStreamJSONLineMax (16MiB). The turn is
// failed (and retried) rather than completed: the CLI was killed mid-stream,
// so any text parsed before the oversized frame is PARTIAL, and reporting it
// as a successful turn would let the judgment-only acceptance check
// (mechanicalAcceptanceCheck: non-empty output ⇒ done) mark a step complete
// on the strength of a forcibly-terminated worker. That partial text is
// discarded entirely — it reaches neither AssistantOutput nor rawOutput — so a
// killed turn can never satisfy verification.
var ErrStreamJSONLineTooLong = errors.New("provider: stream-json line exceeded 16MiB limit")

var declarativeTokens = []string{
	"allowed_tools",
	"effort",
	"model",
	"output_file",
	"prompt",
	"prompt_file",
	"schema_file",
	"system_prompt",
	"user_prompt",
	"working_dir",
}

// DeclarativeRunner executes a config-defined provider binding. It supports a
// small set of framing modes that cover the common provider CLI shapes without
// requiring a custom Go runner.
type DeclarativeRunner struct{}

// Run executes one declarative provider turn.
func (DeclarativeRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	if err := ValidateBinding(binding); err != nil {
		return Result{}, err
	}
	limits, err := ResolveTurnLimits(binding, req)
	if err != nil {
		return Result{}, err
	}
	ctx, cancelTurn := WithTurnDeadline(ctx, limits.TurnTimeout)
	defer cancelTurn()

	attempts := max(1, binding.Config.MaxRetries+1)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		result, err := runDeclarativeAttempt(ctx, binding, req, limits.StallTimeout)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return Result{}, lastErr
}

func runDeclarativeAttempt(ctx context.Context, binding Binding, req Request, stallTimeout time.Duration) (Result, error) {
	var cleanups []func()
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()

	promptPath, err := writeProviderTempFile("prompt.txt", combinePrompt(req))
	if err != nil {
		return Result{}, err
	}
	cleanups = append(cleanups, func() { _ = os.RemoveAll(filepath.Dir(promptPath)) })

	schemaPath, schemaCleanup, err := withTempSchema(req)
	if err != nil {
		return Result{}, err
	}
	cleanups = append(cleanups, schemaCleanup)

	outputPath := ""
	if binding.Config.Type == declarativeLastMessageFile {
		outputPath = binding.Config.OutputFile
		if outputPath == "" {
			dir, err := os.MkdirTemp("", "radioactive_ralph-provider-output-*")
			if err != nil {
				return Result{}, fmt.Errorf("provider: create output dir: %w", err)
			}
			cleanups = append(cleanups, func() { _ = os.RemoveAll(dir) })
			outputPath = filepath.Join(dir, "last-message.txt")
		}
	}

	// One resolution, shared by the token map and the Result, so a declarative
	// binding enforces StrictBinding and reports what actually ran. Calling the
	// raw resolvers from declarativeTokenValues bypassed both: a strict request
	// executed fallback tokens and returned a zero Invocation, so a caller
	// asking for a pinned model got a successful result with no way to learn a
	// different one had run.
	invocation, err := ResolveInvocation(binding, req)
	if err != nil {
		return Result{}, err
	}

	tokens := declarativeTokenValues(req, invocation, promptPath, schemaPath, outputPath)
	if outputPath != "" {
		rendered, err := renderArgTemplate(outputPath, tokens)
		if err != nil {
			return Result{}, fmt.Errorf("provider %q output_file: %w", binding.Name, err)
		}
		outputPath = rendered
		tokens["output_file"] = outputPath
	}

	args, err := renderArgTemplates(binding.Config.Args, tokens)
	if err != nil {
		return Result{}, fmt.Errorf("provider %q args: %w", binding.Name, err)
	}
	if len(args) == 0 {
		args = []string{combinePrompt(req)}
	}

	switch binding.Config.Type {
	case declarativePlainStdout:
		out, err := runCommandWithStallContained(
			ctx, stallTimeout, req.WorkingDir, req.ContainmentRoot, binding.Config.Binary, args)
		if err != nil {
			return Result{}, err
		}
		return Result{
			SessionID:       extractDeclarativeSessionID(binding, out),
			AssistantOutput: normalizeStructuredOutput(out, req),
			Invocation:      invocation,
		}, nil
	case declarativeLastMessageFile:
		if _, err := runCommandWithStallContained(
			ctx, stallTimeout, req.WorkingDir, req.ContainmentRoot, binding.Config.Binary, args,
		); err != nil {
			return Result{}, err
		}
		raw, err := os.ReadFile(outputPath) //nolint:gosec // provider-configured path after templating
		if err != nil {
			return Result{}, fmt.Errorf("provider: read output_file %s: %w", outputPath, err)
		}
		out := strings.TrimSpace(string(raw))
		return Result{
			SessionID:       extractDeclarativeSessionID(binding, out),
			AssistantOutput: normalizeStructuredOutput(out, req),
			Invocation:      invocation,
		}, nil
	case declarativeStreamJSON:
		out, raw, err := runStreamJSONCommand(
			ctx, stallTimeout, req.WorkingDir, req.ContainmentRoot, binding.Config.Binary, args)
		if err != nil {
			// raw is empty on error by runStreamJSONCommand's contract (diagnostic
			// context is folded into err), so there is nothing to extract here.
			return Result{}, err
		}
		return Result{
			SessionID:       extractDeclarativeSessionID(binding, raw),
			AssistantOutput: normalizeStructuredOutput(out, req),
			Invocation:      invocation,
		}, nil
	default:
		return Result{}, fmt.Errorf("provider %q: unsupported declarative type %q", binding.Name, binding.Config.Type)
	}
}

// ValidateBinding validates the parts of a binding that can be checked without
// spawning a provider turn.
func ValidateBinding(binding Binding) error {
	cfg := binding.Config
	// Trust boundary: committed config.toml may only name a shipped
	// provider binary (claude/codex). Any other binary — for a
	// built-in or a declarative type — must come from the gitignored
	// local.toml provider_binary override, so a pull request cannot point
	// the runtime at /bin/sh or another arbitrary executable.
	if err := validateBinaryTrust(binding); err != nil {
		return err
	}
	switch cfg.Type {
	case "", "claude", "codex":
		return nil
	case declarativePlainStdout, declarativeLastMessageFile, declarativeStreamJSON:
	default:
		return fmt.Errorf("provider %q: unsupported provider type %q", binding.Name, cfg.Type)
	}
	if cfg.Binary == "" {
		return fmt.Errorf("provider %q: binary is required", binding.Name)
	}
	for _, arg := range cfg.Args {
		if err := validateArgTemplate(arg); err != nil {
			return fmt.Errorf("provider %q: %w", binding.Name, err)
		}
	}
	if cfg.OutputFile != "" {
		if err := validateArgTemplate(cfg.OutputFile); err != nil {
			return fmt.Errorf("provider %q output_file: %w", binding.Name, err)
		}
	}
	if cfg.Type == declarativeLastMessageFile && cfg.OutputFile == "" {
		hasToken := false
		for _, arg := range cfg.Args {
			if strings.Contains(arg, "{output_file}") {
				hasToken = true
				break
			}
		}
		if !hasToken {
			return fmt.Errorf("provider %q: last-message-file bindings need output_file or an args token {output_file}", binding.Name)
		}
	}
	if cfg.SessionIDRegex != "" {
		if _, err := regexp.Compile(cfg.SessionIDRegex); err != nil {
			return fmt.Errorf("provider %q: compile session_id_regex: %w", binding.Name, err)
		}
	}
	if _, err := exec.LookPath(cfg.Binary); err != nil {
		return fmt.Errorf("provider %q: binary %q not on PATH", binding.Name, cfg.Binary)
	}
	if _, err := ResolveTurnLimits(binding, Request{}); err != nil {
		return err
	}
	return nil
}

// validateBinaryTrust enforces that a committed config.toml cannot name an
// arbitrary executable. An empty binary is left to the type-specific
// checks below. A binary supplied by local.toml (BinaryFromLocal) is
// trusted — that file is gitignored and operator-owned. Otherwise the
// binary must be one of the shipped provider names; anything else
// (absolute paths, /bin/sh, a wrapper script) is refused.
func validateBinaryTrust(binding Binding) error {
	bin := binding.Config.Binary
	if bin == "" || binding.BinaryFromLocal {
		return nil
	}
	if shippedProviderBinaries[bin] {
		return nil
	}
	return fmt.Errorf(
		"provider %q: committed config.toml may not set binary %q; only %s are allowed in config.toml, put any other binary in the gitignored local.toml provider_binary",
		binding.Name, bin, shippedProviderList())
}

func shippedProviderList() string {
	names := make([]string, 0, len(shippedProviderBinaries))
	for name := range shippedProviderBinaries {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func declarativeTokenValues(req Request, invocation Invocation, promptPath, schemaPath, outputPath string) map[string]string {
	return map[string]string{
		"allowed_tools": strings.Join(req.AllowedTools, ","),
		"effort":        invocation.Effort,
		"model":         invocation.Model,
		"output_file":   outputPath,
		"prompt":        combinePrompt(req),
		"prompt_file":   promptPath,
		"schema_file":   schemaPath,
		"system_prompt": req.SystemPrompt,
		"user_prompt":   req.UserPrompt,
		"working_dir":   req.WorkingDir,
	}
}

func renderArgTemplates(args []string, tokens map[string]string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		rendered, err := renderArgTemplate(arg, tokens)
		if err != nil {
			return nil, err
		}
		out = append(out, rendered)
	}
	return out, nil
}

func renderArgTemplate(input string, tokens map[string]string) (string, error) {
	if err := validateArgTemplate(input); err != nil {
		return "", err
	}
	var out strings.Builder
	out.Grow(len(input))
	for len(input) > 0 {
		open := strings.Index(input, "{")
		if open < 0 {
			out.WriteString(input)
			break
		}
		out.WriteString(input[:open])
		closeIdx := strings.Index(input[open:], "}")
		token := input[open+1 : open+closeIdx]
		out.WriteString(tokens[token])
		input = input[open+closeIdx+1:]
	}
	return out.String(), nil
}

func validateArgTemplate(input string) error {
	for {
		open := strings.Index(input, "{")
		if open < 0 {
			return nil
		}
		closeIdx := strings.Index(input[open:], "}")
		if closeIdx < 0 {
			return fmt.Errorf("unterminated template token in %q", input)
		}
		token := input[open+1 : open+closeIdx]
		if !slices.Contains(declarativeTokens, token) {
			return fmt.Errorf("unknown template token {%s}", token)
		}
		input = input[open+closeIdx+1:]
	}
}

// runStreamJSONCommand runs a stream-json provider turn. rawOutput is the
// concatenated raw frames and is meaningful ONLY on success (err == nil) —
// where the caller uses it to extract a session id; on every error path it is
// returned empty, because a failed turn has no usable output and any diagnostic
// context (stderr, a scan error) is folded into the returned error instead.
// containmentRoot is threaded through for the same reason the other two
// declarative shapes take it: the confinement contract is "the provider process
// AND everything it spawns", and a shape that skipped it would leave stream-json
// bindings running unconfined while the config said otherwise. applyContainment
// FAILS CLOSED — an unsupported platform or an unusable root returns an error
// here rather than launching the turn unwrapped.
func runStreamJSONCommand(ctx context.Context, stallTimeout time.Duration, dir, containmentRoot, bin string, args []string) (assistantText, rawOutput string, err error) {
	bin, args, wrapErr := applyContainment(containmentRoot, bin, args)
	if wrapErr != nil {
		return "", "", wrapErr
	}
	stallCtx, progress, cancelStall := withProgressLease(ctx, stallTimeout)
	defer cancelStall()
	cmd := exec.CommandContext(stallCtx, bin, args...) //nolint:gosec // argv is runtime-controlled
	cmd.Dir = dir
	setProcessGroupKill(cmd) // ctx-cancel must reap the whole tree, not just the CLI
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("provider: stdout pipe: %w", err)
	}
	var stderr strings.Builder
	var stderrN int
	cmd.Stderr = progressWriter{
		Writer: &stderr, progress: progress,
		limit: maxAuthoritativeResultBytes, n: &stderrN,
	}
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("provider: start %s: %w", bin, err)
	}

	var assistant strings.Builder
	var raw strings.Builder
	scanner := bufio.NewScanner(progressReader{Reader: stdout, progress: progress})
	scanner.Buffer(make([]byte, 0, 64*1024), declarativeStreamJSONLineMax)
	// declarativeStreamJSONLineMax bounds a SINGLE line. The aggregates below
	// need their own ceiling: a provider emitting many small frames renews the
	// stall lease on every one, so without this the builders grow until the turn
	// deadline — up to 24h under the configurable limits — and can OOM the
	// supervisor. Crossing the ceiling FAILS the turn (retryable) rather than
	// reporting partial frames as a success, matching the oversized-line policy
	// immediately below.
	var aggregateOverflow bool
	for scanner.Scan() {
		line := scanner.Text()
		if raw.Len()+len(line)+1 > maxStructuredEvidenceBytes ||
			assistant.Len() > maxAuthoritativeResultBytes {
			aggregateOverflow = true
			break
		}
		raw.WriteString(line)
		raw.WriteByte('\n')
		if text := extractDeclarativeAssistantText([]byte(line)); text != "" {
			assistant.WriteString(text)
		}
	}
	if aggregateOverflow {
		// Same shutdown discipline as the oversized-line path: we stopped
		// reading, so the CLI may block writing into a full pipe and a plain
		// Wait would hang.
		if cmd.Process != nil {
			_ = killProcessTree(cmd.Process)
		}
		_ = cmd.Wait()
		if cause := context.Cause(stallCtx); cause != nil {
			return "", "", cause
		}
		return "", "", ErrProviderOutputTooLarge
	}
	if scanErr := scanner.Err(); scanErr != nil {
		// We've stopped reading stdout, so the CLI may still be blocked WRITING the
		// rest of an oversized line into a full pipe — a plain cmd.Wait() would
		// hang. Kill the process tree first so Wait returns promptly.
		if cmd.Process != nil {
			_ = killProcessTree(cmd.Process)
		}
		_ = cmd.Wait()
		if cause := context.Cause(stallCtx); cause != nil {
			return "", "", cause
		}
		if errors.Is(scanErr, bufio.ErrTooLong) {
			// A single stream-json line exceeded declarativeStreamJSONLineMax
			// (16MiB), so we killed the CLI mid-stream. FAIL the turn (Run retries,
			// then surfaces the error) rather than reporting the frames parsed
			// before the oversized line as a success: that text is partial, and a
			// nil error would feed it into Evidence.Output where the judgment-only
			// acceptance check would mark the step done on a forcibly-terminated
			// worker. It never reaches AssistantOutput. rawOutput is empty on
			// error per this function's contract.
			return "", "", ErrStreamJSONLineTooLong
		}
		// cmd.Wait() has already run, so stderr is complete: fold it into the
		// error for diagnostics rather than discarding it.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", "", fmt.Errorf("provider: scan stream-json: %w\n%s", scanErr, msg)
		}
		return "", "", fmt.Errorf("provider: scan stream-json: %w", scanErr)
	}
	if err := cmd.Wait(); err != nil {
		if cause := context.Cause(stallCtx); cause != nil {
			return "", "", cause
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(raw.String())
		}
		if msg == "" {
			// No stderr and no captured output: don't append a bare trailing
			// newline to the error.
			return "", "", fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
		}
		return "", "", fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, msg)
	}
	return strings.TrimSpace(assistant.String()), raw.String(), nil
}

func extractDeclarativeAssistantText(raw json.RawMessage) string {
	var frame struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content string          `json:"content"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil || frame.Type != "assistant" {
		return ""
	}
	if text := extractAssistantText(frame.Message); text != "" {
		return text
	}
	if frame.Text != "" {
		return frame.Text
	}
	return frame.Content
}

func extractDeclarativeSessionID(binding Binding, raw string) string {
	if binding.Config.SessionIDRegex == "" {
		return ""
	}
	re := regexp.MustCompile(binding.Config.SessionIDRegex)
	matches := re.FindStringSubmatch(raw)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func writeProviderTempFile(name, content string) (string, error) {
	dir, err := os.MkdirTemp("", "radioactive_ralph-provider-*")
	if err != nil {
		return "", fmt.Errorf("provider: create temp dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("provider: write temp file: %w", err)
	}
	return path, nil
}
