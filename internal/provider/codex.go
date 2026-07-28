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
)

func codexWatchdogConfig(stall time.Duration) agent.WatchdogConfig {
	return streamJSONWatchdogConfigWithStall(stall)
}

// Run executes one non-interactive Codex turn.
func (CodexRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	limits, err := ResolveTurnLimits(binding, req)
	if err != nil {
		return Result{}, err
	}
	ctx, cancelTurn := WithTurnDeadline(ctx, limits.TurnTimeout)
	defer cancelTurn()

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

	// One resolution, reported on the Result and enforcing StrictBinding.
	invocation, err := ResolveInvocation(binding, req)
	if err != nil {
		return Result{}, err
	}
	args := append(codexArgs(binding, req, invocation, outPath, schemaPath), combinePrompt(req))

	a, err := agent.Start(ctx, agent.Options{
		Command:                 binding.Config.Binary,
		Args:                    args,
		Dir:                     req.WorkingDir,
		ContainmentRoot:         req.ContainmentRoot,
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
		codexWatchdogConfig(limits.StallTimeout),
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
	return Result{
		AssistantOutput: normalizeStructuredOutput(string(raw), req),
		Invocation:      invocation,
	}, nil
}

// codexArgs builds the codex command line for one resolved invocation.
//
// Extracted so the effort mapping is testable without spawning a CLI: the
// resolved effort was previously recorded on Result.Invocation but never
// reached the process, so provenance claimed an effort that had not run.
func codexArgs(binding Binding, req Request, invocation Invocation, outPath, schemaPath string) []string {
	args := []string{
		"exec",
		"--json",
		"--color", "never",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"-C", req.WorkingDir,
		"--output-last-message", outPath,
	}
	if invocation.Model != "" {
		args = append(args, "-m", invocation.Model)
	}
	// codex has no dedicated effort flag; reasoning effort is a CONFIG value
	// (`model_reasoning_effort` in ~/.codex/config.toml) and `-c key=value`
	// overrides config for one invocation. Verified against `codex exec --help`
	// on the installed CLI: "-c, --config <key=value> Override a configuration
	// value that would otherwise be loaded from `~/.codex/config.toml`".
	//
	// "default" is skipped deliberately: it names codex's own configured lane
	// rather than a value Ralph translates, so overriding it would REPLACE the
	// operator's config.toml setting with a literal that is not a valid effort.
	if invocation.Effort != "" && invocation.Effort != "default" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", invocation.Effort))
	}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	return append(args, binding.Config.Args...)
}
