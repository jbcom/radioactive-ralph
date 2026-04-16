package provider

import (
	"context"
)

// GeminiRunner executes a single `gemini -p` turn.
type GeminiRunner struct{}

// Run executes one non-interactive Gemini turn.
func (GeminiRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	args := []string{
		"-p", combinePrompt(req),
		"--approval-mode", "yolo",
		"--output-format", "text",
	}
	model := resolveModel(binding.Config, req.Model)
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, binding.Config.Args...)
	out, err := runCommand(ctx, req.WorkingDir, binding.Config.Binary, args)
	if err != nil {
		return Result{}, err
	}
	return Result{AssistantOutput: normalizeStructuredOutput(out, req)}, nil
}
