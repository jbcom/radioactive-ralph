package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// CodexRunner executes a single `codex exec` turn.
//
// codex, like claude/opencode, now runs under Ralph's own pty via
// internal/agent so its pane/stream goes through the same
// superviseAgent-enforced watchdog (spec §1's never-block control
// invariant): a stalled or (despite
// --dangerously-bypass-approvals-and-sandbox) unexpectedly interactive
// codex process is killed rather than left to hang, exactly like
// claude/opencode. codex's own native result channel — the
// --output-last-message file — is unaffected: it is still the
// authoritative source for AssistantOutput, read back from disk after the
// process exits. The pty pane output itself carries no structured result
// for codex (unlike claude/opencode's stream-json), so onLine here has
// nothing to parse; it exists purely so superviseAgent's watchdog has
// output to observe for stall/prompt detection.
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

	a, err := agent.Start(ctx, agent.Options{
		Command:    binding.Config.Binary,
		Args:       args,
		Dir:        req.WorkingDir,
		ResultPath: outPath,
	})
	if err != nil {
		return Result{}, fmt.Errorf("provider: start codex agent: %w", err)
	}
	defer func() { _ = a.Kill() }()

	// codex's pane carries no structured per-line result (unlike
	// claude/opencode's stream-json), so onLine is nil: superviseAgent
	// still runs agent.Watch over the pane for stall/prompt detection, it
	// just has nothing extra to extract per line. superviseAgent returns
	// once a.Output() closes (codex exited on its own) or kills+errors on
	// a detected prompt/stall.
	if err := superviseAgent(ctx, a, DefaultWatchdogConfig(), nil); err != nil {
		return Result{}, fmt.Errorf("provider: codex run: %w", err)
	}

	// codex has no structured terminal frame, so superviseAgent returns nil the
	// moment the process exits — INCLUDING a nonzero exit (auth/model error,
	// mid-run crash). Without this gate, a failed codex that wrote a partial
	// diagnostic to outPath would be laundered into a successful, zero-cost turn
	// (also defeating spend accounting). Fail the turn on a nonzero exit; a kill
	// is reported as nil by ExitErr (superviseAgent already surfaced it above).
	if exitErr := a.ExitErr(); exitErr != nil {
		return Result{}, fmt.Errorf("provider: codex exited nonzero: %w", exitErr)
	}

	raw, err := os.ReadFile(outPath) //nolint:gosec // temporary file owned by this process
	if err != nil {
		return Result{}, fmt.Errorf("provider: read codex output: %w", err)
	}
	return Result{AssistantOutput: normalizeStructuredOutput(string(raw), req)}, nil
}
