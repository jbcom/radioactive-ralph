package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func combinePrompt(req Request) string {
	var b strings.Builder
	if req.SystemPrompt != "" {
		b.WriteString("SYSTEM INSTRUCTIONS:\n")
		b.WriteString(req.SystemPrompt)
		b.WriteString("\n\n")
	}
	if req.Effort != "" {
		fmt.Fprintf(&b, "REASONING EFFORT TARGET: %s\n\n", req.Effort)
	}
	b.WriteString("USER TASK:\n")
	b.WriteString(req.UserPrompt)
	return strings.TrimSpace(b.String())
}

func runCommand(ctx context.Context, dir, bin string, args []string) (string, error) {
	return runCommandWithStall(ctx, DefaultStallTimeout, dir, bin, args)
}

func runCommandWithStall(ctx context.Context, stallTimeout time.Duration, dir, bin string, args []string) (string, error) {
	return runCommandWithStallContained(ctx, stallTimeout, dir, "", bin, args)
}

// runCommandWithStallContained is runCommandWithStall with an optional
// kernel-enforced write boundary. An empty containmentRoot leaves the process
// unconfined, exactly as before.
func runCommandWithStallContained(
	ctx context.Context,
	stallTimeout time.Duration,
	dir, containmentRoot, bin string,
	args []string,
) (string, error) {
	bin, args, wrapErr := applyContainment(containmentRoot, bin, args)
	if wrapErr != nil {
		return "", wrapErr
	}
	stallCtx, progress, cancelStall := withProgressLease(ctx, stallTimeout)
	defer cancelStall()
	cmd := exec.CommandContext(stallCtx, bin, args...) //nolint:gosec // argv is runtime-controlled
	cmd.Dir = dir
	setProcessGroupKill(cmd) // ctx-cancel must reap the whole tree, not just the CLI
	// Capture stdout and stderr separately so on the success path, some
	// CLIs don't get warnings/progress lines folded into AssistantOutput.
	// On failure we surface stderr in the wrapped error so operators can
	// see why the CLI exited non-zero.
	// Bound both sinks as they stream. exec.Cmd aborts the copy and kills the
	// process when a writer returns an error, so crossing the ceiling ends the
	// turn instead of growing until the process OOMs the supervisor.
	var stdout, stderr strings.Builder
	var stdoutN, stderrN int
	cmd.Stdout = progressWriter{
		Writer: &stdout, progress: progress,
		limit: maxAuthoritativeResultBytes, n: &stdoutN,
	}
	cmd.Stderr = progressWriter{
		Writer: &stderr, progress: progress,
		limit: maxAuthoritativeResultBytes, n: &stderrN,
	}
	err := cmd.Run()
	if err != nil {
		// A ceiling crossing is the authoritative reason the turn ended, ahead
		// of the exec error it caused. Report the static sentinel so no
		// provider-controlled bytes ride out on the error path.
		if stdoutN >= maxAuthoritativeResultBytes || stderrN >= maxAuthoritativeResultBytes {
			return "", ErrProviderOutputTooLarge
		}
		if cause := context.Cause(stallCtx); cause != nil {
			return "", cause
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func withTempSchema(req Request) (schemaPath string, cleanup func(), err error) {
	if strings.TrimSpace(req.OutputSchema) == "" {
		return "", func() {}, nil
	}
	tmpDir, err := os.MkdirTemp("", "radioactive_ralph-provider-*")
	if err != nil {
		return "", nil, fmt.Errorf("provider: create temp dir: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }
	schemaPath = filepath.Join(tmpDir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(req.OutputSchema), 0o600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("provider: write schema: %w", err)
	}
	return schemaPath, cleanup, nil
}

func normalizeStructuredOutput(raw string, req Request) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if req.OutputSchema == "" && !strings.Contains(raw, "```") {
		return raw
	}
	openIdx := strings.Index(raw, "{")
	closeIdx := strings.LastIndex(raw, "}")
	if openIdx < 0 || closeIdx < 0 || closeIdx < openIdx {
		return raw
	}
	return strings.TrimSpace(raw[openIdx : closeIdx+1])
}
