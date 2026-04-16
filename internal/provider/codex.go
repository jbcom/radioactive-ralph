package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CodexRunner executes a single `codex exec` turn.
type CodexRunner struct{}

// Run executes one non-interactive Codex turn.
func (CodexRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	schemaPath, cleanup, err := withTempSchema(req)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	tmpDir := filepath.Dir(schemaPath)
	if tmpDir == "." || tmpDir == "" {
		tmpDir, err = os.MkdirTemp("", "radioactive_ralph-codex-*")
		if err != nil {
			return Result{}, fmt.Errorf("provider: create codex temp dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()
	}
	outPath := filepath.Join(tmpDir, "last-message.txt")

	args := []string{
		"exec",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-C", req.WorkingDir,
		"--output-last-message", outPath,
	}
	model := resolveModel(binding.Config, req.Model)
	if model != "" {
		args = append(args, "-m", model)
	}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, binding.Config.Args...)
	args = append(args, combinePrompt(req))

	if _, err := runCommand(ctx, req.WorkingDir, binding.Config.Binary, args); err != nil {
		return Result{}, err
	}
	raw, err := os.ReadFile(outPath) //nolint:gosec // temporary file owned by this process
	if err != nil {
		return Result{}, fmt.Errorf("provider: read codex output: %w", err)
	}
	return Result{AssistantOutput: normalizeStructuredOutput(string(raw), req)}, nil
}
