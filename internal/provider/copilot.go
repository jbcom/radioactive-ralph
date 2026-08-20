package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// CopilotRunner executes a single non-interactive `copilot -p` turn.
//
// Verified directly against the real, installed `@github/copilot` CLI
// (v1.0.80) before writing this, not assumed from documentation: its
// --output-format json stream uses a DIFFERENT event schema than
// claude/codex's `{"type":"assistant",...}` convention -- Copilot nests
// everything under "data" with dotted type names
// (assistant.message/assistant.message_start/session.mcp_servers_loaded/
// etc.), and its authoritative final answer is the non-ephemeral
// "assistant.message" frame's data.content, with a trailing non-ephemeral
// "result" frame carrying sessionId and exitCode. There is no
// --output-last-message-style file flag the way codex has, so (unlike
// codex.go) the assistant text comes from the JSONL stream itself, not a
// separate result file. A config-time failure (e.g. an unknown --model)
// prints plain text to stderr with NO JSON at all and a nonzero exit --
// confirmed directly, not assumed -- so failure detection here is
// exit-code-and-missing-result-frame based, not a JSONL error-event scan
// the way codex's is.
//
// Copilot takes its prompt as a plain -p argument, not stdin, so this
// runner never touches agent.Options.OneShotInput -- one fewer moving part
// than claude's stream-json-over-stdin protocol, and it works identically
// whether agent.Start dispatches natively or (on Windows) through the
// managed WSL2 distro, since it never needs the stdin channel at all.
type CopilotRunner struct{}

// ErrCopilotTurnFailed is returned when copilot exits nonzero without a
// successful "result" frame. Provider-controlled stderr text never crosses
// this boundary as the error identity, only as folded-in diagnostic context.
var ErrCopilotTurnFailed = errors.New("provider: copilot reported a failed turn")

const copilotRetainedJSONLLineBytes = 4 << 20 // matches codex's rationale; no larger frame observed empirically

func copilotWatchdogConfig(stall time.Duration) agent.WatchdogConfig {
	return streamJSONWatchdogConfigWithStall(stall)
}

// Run executes one non-interactive Copilot turn.
func (CopilotRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	limits, err := ResolveTurnLimits(binding, req)
	if err != nil {
		return Result{}, err
	}
	ctx, cancelTurn := WithTurnDeadline(ctx, limits.TurnTimeout)
	defer cancelTurn()

	invocation, err := ResolveInvocation(binding, req)
	if err != nil {
		return Result{}, err
	}
	args := copilotArgs(binding, req, invocation)
	hookEnv, err := managedHookEnvironment(req)
	if err != nil {
		return Result{}, err
	}

	a, err := agent.Start(ctx, agent.Options{
		Command:                 binding.Config.Binary,
		Args:                    args,
		Dir:                     req.WorkingDir,
		ContainmentRoot:         req.ContainmentRoot,
		ContainmentWritePaths:   BindingWritePaths(binding),
		MaxOutputRetentionBytes: agent.RetentionBudgetForLineBytes(copilotRetainedJSONLLineBytes),
		OversizeOutputPolicy:    agent.DiscardOversizeOutput,
		MaxObservedOutputBytes:  maxStructuredEvidenceBytes,
		Env:                     hookEnv,
	})
	if err != nil {
		return Result{}, fmt.Errorf("provider: start copilot agent: %w", err)
	}

	var collector copilotResultCollector
	if err := superviseAgentWithDiscarded(
		ctx,
		a,
		copilotWatchdogConfig(limits.StallTimeout),
		func(line []byte) bool {
			collector.consume(line)
			return collector.failed()
		},
		func([]byte) bool { return false }, // discarded oversize prefixes carry no signal here
	); err != nil {
		return Result{}, fmt.Errorf("provider: copilot run: %w", err)
	}

	exitErr := a.ExitErr()
	if !collector.succeeded() {
		// Deliberately no raw provider output in this error, matching
		// codex.go's own boundary: "provider text, arbitrary terminal
		// output... [is] never promoted into errors." A config-time failure
		// (bad --model, etc.) prints plain text to the merged stdout+stderr
		// stream with no JSON at all -- confirmed directly -- so there is
		// nothing structured to classify beyond "no result frame arrived."
		if exitErr != nil {
			return Result{}, fmt.Errorf("%w (no result frame observed): %w", ErrCopilotTurnFailed, exitErr)
		}
		return Result{}, fmt.Errorf("%w: no result frame observed", ErrCopilotTurnFailed)
	}
	if exitErr != nil {
		// A "result" frame with exitCode 0 arrived, but Ralph's own process
		// wait disagrees -- trust the disagreement rather than the
		// self-reported frame; surface both for diagnosis.
		return Result{}, fmt.Errorf("provider: copilot exited nonzero despite a successful result frame: %w", exitErr)
	}

	return Result{
		SessionID:       collector.sessionID,
		AssistantOutput: normalizeStructuredOutput(collector.assistantText, req),
		Invocation:      invocation,
	}, nil
}

// copilotArgs builds the copilot command line for one resolved invocation.
func copilotArgs(binding Binding, req Request, invocation Invocation) []string {
	args := []string{
		"-p", combinePrompt(req),
		"--output-format", "json",
		"-s", // silent: agent response only, no stats noise mixed into JSON stdout
		"--no-color",
		"--allow-all-tools", // required for non-interactive mode per `copilot --help`
	}
	if req.WorkingDir != "" {
		args = append(args, "-C", req.WorkingDir)
	}
	if invocation.Model != "" {
		args = append(args, "--model", invocation.Model)
	}
	// "default" names copilot's own configured lane, same reasoning as
	// codexArgs skipping it: Ralph should not override an operator setting
	// with a literal that isn't a valid effort value.
	if invocation.Effort != "" && invocation.Effort != "default" {
		args = append(args, "--effort", invocation.Effort)
	}
	return append(args, binding.Config.Args...)
}

// copilotResultCollector scans copilot's --output-format json stream for the
// two non-ephemeral frame types that carry authoritative information: the
// final "assistant.message" (the answer) and the trailing "result" (exit
// status + session id). Every other frame type (session.*, model.*,
// assistant.message_start/_delta, assistant.reasoning*, mcp.*) is
// intermediate/ephemeral progress noise, verified directly against the real
// CLI, and is not inspected for content.
type copilotResultCollector struct {
	assistantText string
	sessionID     string
	resultSeen    bool
	exitCode      int
}

type copilotFrame struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	SessionID string          `json:"sessionId"`
	ExitCode  *int            `json:"exitCode"`
}

type copilotAssistantMessageData struct {
	Content string `json:"content"`
}

func (c *copilotResultCollector) consume(line []byte) {
	var f copilotFrame
	if err := json.Unmarshal(line, &f); err != nil {
		// agent.Start merges stdout+stderr into one stream (matching a Unix
		// pty master's single combined channel), so a config-time error
		// (e.g. an unknown --model, confirmed to print plain text with no
		// JSON at all) DOES arrive here as a non-JSON line. Matching
		// codex.go's own boundary ("provider text... never promoted into
		// errors"), it is deliberately not captured or classified -- only
		// the presence/absence of a valid "result" frame decides
		// succeeded()/failed().
		return
	}
	switch f.Type {
	case "assistant.message":
		var data copilotAssistantMessageData
		if err := json.Unmarshal(f.Data, &data); err == nil {
			// Last-write-wins, not accumulation (unlike claude's runner,
			// which does accumulate). This is deliberate, not merely
			// "matches what's been observed so far": the trailing "result"
			// frame's exitCode is the authoritative completion signal for
			// this runner (see succeeded()/failed() below), and
			// assistant.message is documented and observed as the turn's
			// FINAL answer, not an intermediate step in a visible reasoning
			// trace the way, say, assistant.reasoning_delta frames are (see
			// this file's package doc for the full frame-type evidence).
			// If a future Copilot release starts emitting genuinely
			// separate multi-step assistant.message frames within one
			// turn, this would need to switch to accumulation like
			// claude's -- flagged here so that's a deliberate future
			// decision, not a silent regression nobody notices.
			c.assistantText = data.Content
		}
	case "result":
		c.resultSeen = true
		c.sessionID = f.SessionID
		if f.ExitCode != nil {
			c.exitCode = *f.ExitCode
		}
	}
}

// consumeDiscardedPrefix intentionally does nothing: an oversized frame
// discarded here means only that one intermediate/ephemeral event exceeded
// the retention budget, not that the turn's authoritative "assistant.message"
// or "result" frame did -- those are asserted comfortably within
// copilotRetainedJSONLLineBytes by every real invocation observed.
func (c *copilotResultCollector) failed() bool    { return c.resultSeen && c.exitCode != 0 }
func (c *copilotResultCollector) succeeded() bool { return c.resultSeen && c.exitCode == 0 }
