package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// OpencodeRunner executes a single `opencode run --format json` turn under
// Ralph's own pty via internal/agent, per spec §9 ("opencode bound via its
// local `run` path only") and §3 (hybrid I/O).
//
// Verified against the installed `opencode` 1.18.3 CLI on 2026-07-16:
// `opencode run [message..] --format json` emits one JSON object per line
// on stdout (never a file — there is no output-file flag), each with a
// top-level "type": "step_start" | "text" | "step_finish" | others. The
// assistant reply lives in `type":"text"` frames' part.text; token/cost
// usage lives in the `type":"step_finish"` frame's part.tokens
// (input/output/cache.read) and part.cost. `--session`/`--continue`
// resumes a session, `--variant` sets reasoning effort, `--dir` sets the
// working directory, `--model` takes `provider/model`.
//
// Like ClaudeRunner, there is no CLI-native result-file flag, so
// ResultPath is Ralph-side: the runner tees recognized JSON frames from
// the pty's Output() into a bounded ResultPath evidence file while parsing
// every text and step_finish frame. It consumes until the CLI exits naturally
// after session idle, then validates the final step reason.
type OpencodeRunner struct{}

// ErrOpencodeReportedError is returned for a type=error session event. The
// provider's nested name/message deliberately never crosses this boundary.
var ErrOpencodeReportedError = errors.New("provider: opencode reported a session error")

// ErrOpencodeFinalReason means the final step was not stop or length.
var ErrOpencodeFinalReason = errors.New("provider: opencode exited without a final stop or length step")

// ErrOpencodeMissingFinish means the natural stream ended without any
// step_finish event.
var ErrOpencodeMissingFinish = errors.New("provider: opencode exited without a step_finish frame")

// ErrOpencodeInvalidUsage is returned for negative, non-finite, or overflowing
// aggregate usage.
var ErrOpencodeInvalidUsage = errors.New("provider: opencode reported invalid usage")

// Run spawns `opencode run <prompt> --format json` and blocks until the
// CLI exits naturally. A step_finish with reason=tool-calls is an
// intermediate model step; OpenCode 1.18.3 closes the actual run only after
// session.status becomes idle, so Ralph must consume the complete stream.
func (OpencodeRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	limits, err := ResolveTurnLimits(binding, req)
	if err != nil {
		return Result{}, err
	}
	ctx, cancelTurn := WithTurnDeadline(ctx, limits.TurnTimeout)
	defer cancelTurn()

	resultPath, cleanup, err := newResultFile("opencode-result-*.jsonl")
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	// One resolution, reported on the Result and enforcing StrictBinding.
	invocation, err := ResolveInvocation(binding, req)
	if err != nil {
		return Result{}, err
	}
	hookEnv, err := managedHookEnvironment(req)
	if err != nil {
		return Result{}, err
	}
	launch, err := resolveOpencodeLaunch(
		binding, req, invocation, limits.StallTimeout, os.Getenv, exec.LookPath)
	if err != nil {
		return Result{}, err
	}

	opts := agent.Options{
		Command:               launch.command,
		Args:                  launch.args,
		Dir:                   req.WorkingDir,
		ContainmentRoot:       req.ContainmentRoot,
		ContainmentWritePaths: launch.containmentWritePaths,
		ResultPath:            resultPath,
		// Count raw PTY reads so discarded/partial/non-JSON progress cannot
		// refresh the watchdog indefinitely without consuming a hard budget.
		MaxObservedOutputBytes: maxStructuredEvidenceBytes,
		Env:                    hookEnv,
	}
	a, err := agent.Start(ctx, opts)
	if err != nil {
		return Result{}, fmt.Errorf("provider: start opencode agent: %w", err)
	}

	resultFile, err := newBoundedEvidenceFile(resultPath)
	if err != nil {
		return Result{}, errors.Join(
			fmt.Errorf("provider: create result file: %w", err),
			a.TerminateAndWait(),
		)
	}

	var assistant boundedResultBuffer
	var sessionID string
	var usage Usage
	var sawFinish bool
	var finalReason string
	var sawError bool
	var ingestErr error

	// As in ClaudeRunner, every line is routed through superviseAgent so a
	// hung/prompting opencode CLI is killed per the control invariant
	// (spec §1) instead of hanging this Run call forever.
	onLine := func(line []byte) bool {
		ev, ok := parseOpencodeEvent(line)
		if !ok {
			return false // pty echo / non-JSON pane noise
		}
		if err := resultFile.writeFrame(line); err != nil {
			ingestErr = err
			return true
		}
		if ev.SessionID != "" {
			if len(ev.SessionID) > maxAuthoritativeResultBytes-assistant.n {
				ingestErr = ErrAuthoritativeResultTooLarge
				return true
			}
			sessionID = ev.SessionID
		}
		switch ev.Type {
		case "text":
			if err := assistant.writeStringReserved(
				ev.Part.Text,
				len(sessionID),
			); err != nil {
				ingestErr = err
				return true
			}
		case "step_finish":
			if err := addOpencodeUsage(&usage, ev.Part.usage()); err != nil {
				ingestErr = err
				return true
			}
			sawFinish = true
			finalReason = ev.Part.Reason
		case "error":
			sawError = true
			return true
		}
		return false
	}

	runErr := superviseAgent(ctx, a, streamJSONWatchdogConfigWithStall(limits.StallTimeout), onLine)
	closeErr := resultFile.close()
	if runErr != nil {
		runErr = fmt.Errorf("provider: opencode run: %w", runErr)
		if errors.Is(runErr, agent.ErrObservedOutputTooLarge) {
			runErr = errors.Join(runErr, ErrStructuredEvidenceTooLarge)
		}
	}
	if ingestErr != nil || runErr != nil || closeErr != nil {
		return Result{}, errors.Join(ingestErr, runErr, closeErr)
	}

	exitErr := a.ExitErr()
	if sawError || exitErr != nil {
		var classified error
		if sawError {
			classified = ErrOpencodeReportedError
		}
		if exitErr != nil {
			exitErr = fmt.Errorf("provider: opencode exited nonzero: %w", exitErr)
		}
		return Result{}, errors.Join(classified, exitErr)
	}
	if !sawFinish {
		return Result{}, ErrOpencodeMissingFinish
	}
	if finalReason != "stop" && finalReason != "length" {
		return Result{}, ErrOpencodeFinalReason
	}

	return Result{
		SessionID:       sessionID,
		AssistantOutput: normalizeStructuredOutput(assistant.String(), req),
		Usage:           usage,
		Invocation:      invocation,
	}, nil
}

type opencodePathLookup func(string) (string, error)

type opencodeLaunch struct {
	command               string
	args                  []string
	containmentWritePaths []string
}

func resolveOpencodeLaunch(
	binding Binding,
	req Request,
	invocation Invocation,
	stallTimeout time.Duration,
	getenv adapters.Environment,
	lookPath opencodePathLookup,
) (opencodeLaunch, error) {
	managed := req.ManagedSessionID != "" && req.HookEndpoint != ""
	args := opencodeArgs(binding, req, invocation, managed)
	writePaths := BindingWritePaths(binding)
	if !managed {
		return opencodeLaunch{
			command: binding.Config.Binary, args: args,
			containmentWritePaths: writePaths,
		}, nil
	}
	bundle, err := adapters.CurrentBundleFromEnvironment(getenv)
	if err != nil {
		return opencodeLaunch{}, fmt.Errorf("provider: managed OpenCode adapter unavailable: %w", err)
	}
	realBinary, err := lookPath(binding.Config.Binary)
	if err != nil {
		return opencodeLaunch{}, fmt.Errorf("provider: resolve OpenCode binary: %w", err)
	}
	realBinary, err = filepath.Abs(realBinary)
	if err != nil {
		return opencodeLaunch{}, fmt.Errorf("provider: resolve absolute OpenCode binary: %w", err)
	}
	args = append([]string{
		"hook", "launch-opencode",
		"--binary", realBinary,
		"--adapter-root", bundle.Target,
		"--verification-progress-interval", opencodeVerificationProgressInterval(stallTimeout).String(),
		"--",
	}, args...)
	return opencodeLaunch{
		command: bundle.Executable, args: args,
		containmentWritePaths: []string{bundle.OpenCodeHome, bundle.OpenCodeConfigDir},
	}, nil
}

func opencodeVerificationProgressInterval(stallTimeout time.Duration) time.Duration {
	interval := stallTimeout / 3
	if interval <= 0 {
		return time.Nanosecond
	}
	return interval
}

// opencodeEvent is one `opencode run --format json` stream event.
type opencodeEvent struct {
	Type      string       `json:"type"`
	SessionID string       `json:"sessionID"`
	Part      opencodePart `json:"part"`
}

// opencodePart carries the per-event-type payload. Fields not relevant to
// a given event type are left zero.
type opencodePart struct {
	Text   string `json:"text"`
	Reason string `json:"reason"`
	Tokens struct {
		Total     int `json:"total"`
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Write int `json:"write"`
			Read  int `json:"read"`
		} `json:"cache"`
	} `json:"tokens"`
	Cost float64 `json:"cost"`
}

func (p opencodePart) usage() Usage {
	return Usage{
		InputTokens:       p.Tokens.Input,
		OutputTokens:      p.Tokens.Output,
		CachedInputTokens: p.Tokens.Cache.Read,
		CostUSD:           p.Cost,
	}
}

func addOpencodeUsage(total *Usage, step Usage) error {
	if step.InputTokens < 0 || step.OutputTokens < 0 ||
		step.CachedInputTokens < 0 || step.CostUSD < 0 ||
		math.IsNaN(step.CostUSD) || math.IsInf(step.CostUSD, 0) {
		return ErrOpencodeInvalidUsage
	}
	maxInt := int(^uint(0) >> 1)
	if step.InputTokens > maxInt-total.InputTokens ||
		step.OutputTokens > maxInt-total.OutputTokens ||
		step.CachedInputTokens > maxInt-total.CachedInputTokens ||
		total.CostUSD > math.MaxFloat64-step.CostUSD {
		return ErrOpencodeInvalidUsage
	}
	total.InputTokens += step.InputTokens
	total.OutputTokens += step.OutputTokens
	total.CachedInputTokens += step.CachedInputTokens
	total.CostUSD += step.CostUSD
	return nil
}

// parseOpencodeEvent parses one line of opencode's --format json stream.
// ok is false for non-JSON or type-less lines (pty stdin echo, banner
// noise) so callers can discard them before they reach ResultPath.
func parseOpencodeEvent(line []byte) (ev opencodeEvent, ok bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return opencodeEvent{}, false
	}
	if err := json.Unmarshal(trimmed, &ev); err != nil || ev.Type == "" {
		return opencodeEvent{}, false
	}
	return ev, true
}

// opencodeArgs builds the opencode command line.
//
// Extracted so the argv is testable without spawning a CLI.
func opencodeArgs(binding Binding, req Request, invocation Invocation, managed bool) []string {
	args := []string{
		"run", combinePrompt(req), "--format", "json",
	}
	if !managed {
		// Ordinary turns stay pure. Managed turns instead run through Ralph's
		// absolute launcher, which isolates OpenCode to one reviewed plugin and
		// disables both project and user plugin discovery.
		args = append(args, "--pure")
	}
	if req.WorkingDir != "" {
		args = append(args, "--dir", req.WorkingDir)
	}
	// Model and effort come from the RESOLVED invocation, not a second
	// resolution here: one resolution per turn means what runs and what gets
	// reported cannot disagree.
	if invocation.Model != "" {
		args = append(args, "--model", invocation.Model)
	}
	if invocation.Effort != "" {
		args = append(args, "--variant", invocation.Effort)
	}
	// NOTE: --auto is deliberately NOT passed. See opencode_args_test.go.
	args = append(args, binding.Config.Args...)
	return args
}
