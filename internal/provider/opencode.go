package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

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
// the pty's Output() into the ResultPath file as they arrive, then parses
// the accumulated file for the terminal step_finish frame's usage.
type OpencodeRunner struct{}

// Run spawns `opencode run <prompt> --format json` and blocks until the
// step_finish frame (or process exit) closes the turn.
func (OpencodeRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	resultPath, cleanup, err := newResultFile("opencode-result-*.jsonl")
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	args := []string{"run", combinePrompt(req), "--format", "json"}
	if req.WorkingDir != "" {
		args = append(args, "--dir", req.WorkingDir)
	}
	if model := resolveModel(binding.Config, req.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := resolveEffort(binding.Config, req.Effort); effort != "" {
		args = append(args, "--variant", effort)
	}
	args = append(args, binding.Config.Args...)

	opts := agent.Options{
		Command:    binding.Config.Binary,
		Args:       args,
		Dir:        req.WorkingDir,
		ResultPath: resultPath,
	}
	a, err := agent.Start(ctx, opts)
	if err != nil {
		return Result{}, fmt.Errorf("provider: start opencode agent: %w", err)
	}
	defer func() { _ = a.Kill() }()

	resultFile, err := os.Create(resultPath) //nolint:gosec // Ralph-owned temp file
	if err != nil {
		return Result{}, fmt.Errorf("provider: create result file: %w", err)
	}
	defer func() { _ = resultFile.Close() }()

	var assistant bytes.Buffer
	var sessionID string
	var usage Usage
	var sawFinish bool

	// As in ClaudeRunner, every line is routed through superviseAgent so a
	// hung/prompting opencode CLI is killed per the control invariant
	// (spec §1) instead of hanging this Run call forever.
	onLine := func(line []byte) bool {
		ev, ok := parseOpencodeEvent(line)
		if !ok {
			return false // pty echo / non-JSON pane noise
		}
		_, _ = resultFile.Write(line)
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		switch ev.Type {
		case "text":
			assistant.WriteString(ev.Part.Text)
		case "step_finish":
			usage = ev.Part.usage()
			sawFinish = true
			return true // terminal frame seen; stop supervising this turn
		}
		return false
	}

	if err := superviseAgent(ctx, a, DefaultWatchdogConfig(), onLine); err != nil {
		return Result{}, fmt.Errorf("provider: opencode run: %w", err)
	}
	if !sawFinish {
		return Result{}, fmt.Errorf("provider: opencode exited without a step_finish frame")
	}

	return Result{
		SessionID:       sessionID,
		AssistantOutput: normalizeStructuredOutput(assistant.String(), req),
		Usage:           usage,
	}, nil
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
