package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // argv is runtime-controlled
	cmd.Dir = dir
	// Capture stdout and stderr separately so success-path callers
	// (gemini in particular) don't get warnings/progress lines folded
	// into AssistantOutput. On failure we surface stderr in the
	// wrapped error so operators can see why the CLI exited non-zero.
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
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
