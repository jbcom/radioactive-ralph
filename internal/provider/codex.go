package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// CodexRunner executes a single `codex exec` turn.
//
// Codex runs under Ralph's own pty via internal/agent so its JSONL stream goes
// through the superviseAgent-enforced watchdog (spec §1's never-block control
// invariant). The --output-last-message file remains the sole success-result
// channel. On failure, only the documented error.message fields from "error"
// and "turn.failed" JSONL events are inspected transiently and mapped to a
// closed set of static failure categories. Provider text, arbitrary terminal
// output, and partial last-message files are never promoted into errors.
type CodexRunner struct{}

// ErrCodexTurnFailed is returned when Codex emits its authoritative
// turn.failed event. Provider-controlled error text never crosses this
// boundary.
var ErrCodexTurnFailed = errors.New("provider: codex reported a failed turn")

// ErrCodexOversizeSchema is the fail-closed boundary for a Codex JSON object
// whose top-level type discriminator cannot be trusted. This includes duplicate
// type keys in a fully retained object and discarded objects other than an
// immediately recognizable turn.failed event.
var ErrCodexOversizeSchema = errors.New("provider: codex event has an untrusted type discriminator")

const (
	// Codex 0.145.0 does not publish a JSONL-record cap. Darwin arm64 ARG_MAX is
	// 1 MiB, so 4 MiB covers a full command argv plus common worst-case JSON
	// quote/backslash expansion and its event envelope. It remains below Agent's
	// 8 MiB retained-line maximum. Control-heavy or otherwise larger records are
	// still counted by the unchanged 16 MiB raw ceiling, but structured records
	// beyond this inspection bound fail closed rather than being trusted from a
	// partial prefix.
	codexRetainedJSONLLineBytes = 4 << 20

	// Keep aggressively test-sized global timeouts from treating cold CLI
	// startup as a stall on a loaded host. Once the pty yields bytes, underlying
	// read activity—not retained-line completion—continually resets this timer.
	// Raw prompts are still detected immediately, and the production default
	// (three minutes) is already larger.
	codexMinimumStallTimeout = 10 * time.Second
)

func codexWatchdogConfig() agent.WatchdogConfig {
	cfg := StreamJSONWatchdogConfig()
	if cfg.StallTimeout < codexMinimumStallTimeout {
		cfg.StallTimeout = codexMinimumStallTimeout
	}
	return cfg
}

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
		"--json",
		"--color", "never",
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
		Command:                 binding.Config.Binary,
		Args:                    args,
		Dir:                     req.WorkingDir,
		ResultPath:              outPath,
		MaxOutputRetentionBytes: agent.RetentionBudgetForLineBytes(codexRetainedJSONLLineBytes),
		OversizeOutputPolicy:    agent.DiscardOversizeOutput,
		MaxObservedOutputBytes:  maxStructuredEvidenceBytes,
	})
	if err != nil {
		return Result{}, fmt.Errorf("provider: start codex agent: %w", err)
	}

	var diagnostics codexDiagnosticCollector
	if err := superviseAgentWithDiscarded(
		ctx,
		a,
		codexWatchdogConfig(),
		func(line []byte) bool {
			diagnostics.consume(line)
			return diagnostics.failed()
		},
		diagnostics.consumeDiscardedPrefix,
	); err != nil {
		return Result{}, fmt.Errorf("provider: codex run: %w", err)
	}

	// superviseAgent returns nil whenever the process exits on its own, including
	// a nonzero exit. Fail the turn here and surface only fixed failure
	// categories classified from the two supported JSONL error event shapes. A
	// partial last-message file is never read on this path. String supplies the
	// fixed generic category when no recognized event was observed.
	exitErr := a.ExitErr()
	if diagnosticErr := diagnostics.failure(); diagnosticErr != nil {
		failureErr := fmt.Errorf("%w: %s", diagnosticErr, diagnostics.String())
		if exitErr == nil {
			return Result{}, failureErr
		}
		return Result{}, errors.Join(
			failureErr,
			fmt.Errorf("provider: codex exited nonzero: %w", exitErr),
		)
	}
	if exitErr != nil {
		return Result{}, fmt.Errorf("provider: codex exited nonzero: %w: %s", exitErr, diagnostics.String())
	}

	raw, err := readBoundedAuthoritativeResult(outPath)
	if err != nil {
		return Result{}, fmt.Errorf("provider: read codex output: %w", err)
	}
	return Result{AssistantOutput: normalizeStructuredOutput(string(raw), req)}, nil
}
