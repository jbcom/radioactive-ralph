package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// ClaudeRunner executes a single `claude -p` turn under Ralph's own pty via
// internal/agent, per spec §2/§3: Ralph owns the pty (agent.Start), the
// pane/output stream is for human/watchdog observation, and the
// structured result is read back from a file Ralph passes to the CLI —
// never scraped from the rendered pane.
//
// claude has no native "write result to a file" flag (verified against
// `claude --help` on the installed 2.1.211 CLI: --output-format
// json/stream-json both write to stdout only). So the ResultPath file here
// is Ralph-side, not CLI-native: the runner tees every stdout line (which
// IS the stream-json frames — the same content a human pane would show)
// into req's ResultPath file as it arrives, then parses that file's
// accumulated content for the terminal result frame. This keeps the
// "never scrape the rendered pane for data" invariant: ResultPath holds
// the same raw JSON lines the CLI emitted, not a re-rendered terminal.
type ClaudeRunner struct{}

// Run spawns `claude -p --input-format stream-json --output-format
// stream-json` under agent.Start, feeds req.UserPrompt on stdin via a
// one-shot input file (claude in --input-format stream-json mode reads a
// JSON-line user message from stdin), tees stdout into a ResultPath file,
// and parses the terminal result frame from that file for Usage.
func (ClaudeRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	model := resolveModel(binding.Config, req.Model)
	effort := resolveEffort(binding.Config, req.Effort)

	resultPath, cleanup, err := newResultFile("claude-result-*.jsonl")
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	sessionID := uuid.NewString()
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--session-id", sessionID,
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	for _, t := range req.AllowedTools {
		args = append(args, "--allowed-tools", t)
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
		return Result{}, fmt.Errorf("provider: start claude agent: %w", err)
	}
	defer func() { _ = a.Kill() }()

	if err := sendStreamJSONInput(a, req.UserPrompt); err != nil {
		return Result{}, fmt.Errorf("provider: send claude input: %w", err)
	}

	resultFile, err := os.Create(resultPath) //nolint:gosec // Ralph-owned temp file
	if err != nil {
		return Result{}, fmt.Errorf("provider: create result file: %w", err)
	}
	defer func() { _ = resultFile.Close() }()

	var assistant bytes.Buffer
	var sawResult bool
	var frame claudeResultFrame

	// Every line first passes through superviseAgent, which runs
	// agent.Watch concurrently over a.Output() per the control invariant
	// (spec §1): a permission/clarification prompt or a stall KILLS claude
	// and returns ErrAgentBlocked instead of hanging. Only lines
	// superviseAgent itself classifies as ordinary progress (never a
	// detected prompt) reach onLine, so the JSON stream-framing below is
	// unchanged from the pre-watchdog read loop except for its source.
	onLine := func(line []byte) bool {
		// The pty echoes our own stdin write (stdin/stdout share one fd
		// under a pty, unlike a plain pipe), and claude may print non-JSON
		// banner/warning lines. Only stream-json frames with a recognized
		// top-level "type" are structured data; everything else is pane
		// noise that must not land in ResultPath (the "never
		// scrape/never pollute the structured result file" invariant).
		kind, text, isResult, f := parseClaudeStreamLine(line)
		if kind == "" {
			return false
		}
		// Tee the raw pane line into ResultPath — this is the structured
		// -data path; the same bytes remain available to a.Output()
		// consumers (pane/watchdog) for observation.
		_, _ = resultFile.Write(line)

		if text != "" {
			assistant.WriteString(text)
		}
		if isResult {
			sawResult = true
			frame = f
			return true // terminal frame seen; stop supervising this turn
		}
		return false
	}

	if err := superviseAgent(ctx, a, DefaultWatchdogConfig(), onLine); err != nil {
		return Result{}, fmt.Errorf("provider: claude run: %w", err)
	}
	if !sawResult {
		return Result{}, fmt.Errorf("provider: claude exited without a result frame")
	}

	return Result{
		SessionID:       sessionID,
		AssistantOutput: normalizeStructuredOutput(assistant.String(), req),
		Usage:           frame.usage(),
	}, nil
}

// sendStreamJSONInput writes one Outbound-shaped user message as a JSON
// line to the agent's pty stdin via agent.Agent.WriteInput, matching what
// claudesession.Session sends over a direct pipe today. claude reads
// stream-json input and processes turns as lines arrive, so one write is
// sufficient to drive this one-shot turn.
func sendStreamJSONInput(a *agent.Agent, userPrompt string) error {
	type outboundInner struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	msg := struct {
		Type    string `json:"type"`
		Message outboundInner
	}{Type: "user"}
	msg.Message.Role = "user"
	msg.Message.Content = append(msg.Message.Content, struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "text", Text: userPrompt})
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return a.WriteInput(b)
}

// claudeResultFrame is the terminal `type=result` stream-json frame.
type claudeResultFrame struct {
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (f claudeResultFrame) usage() Usage {
	return Usage{
		InputTokens:       f.Usage.InputTokens,
		OutputTokens:      f.Usage.OutputTokens,
		CachedInputTokens: f.Usage.CacheReadInputTokens + f.Usage.CacheCreationInputTokens,
		CostUSD:           f.TotalCostUSD,
	}
}

// parseClaudeStreamLine parses one stream-json line. kind is the frame's
// "type" field, or "" if line is not a recognized stream-json frame at all
// (pty stdin echo, banner text, blank lines) — callers use kind=="" to
// discard pane noise before it reaches a structured-result sink. For a
// `type=result` line, isResult is true and frame carries the parsed usage.
func parseClaudeStreamLine(line []byte) (kind, assistantText string, isResult bool, frame claudeResultFrame) {
	var envelope struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message,omitempty"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil || envelope.Type == "" {
		return "", "", false, claudeResultFrame{}
	}
	switch envelope.Type {
	case "assistant":
		return envelope.Type, extractAssistantText(envelope.Message), false, claudeResultFrame{}
	case "result":
		var f claudeResultFrame
		_ = json.Unmarshal(line, &f)
		return envelope.Type, "", true, f
	default:
		return envelope.Type, "", false, claudeResultFrame{}
	}
}

// parseClaudeUsage extracts token/cost accounting from a stream-json
// `result` frame. Kept as a standalone helper (in addition to
// claudeResultFrame.usage) because existing tests call it directly with a
// raw frame; both paths parse the identical shape.
func parseClaudeUsage(raw []byte) Usage {
	if len(raw) == 0 {
		return Usage{}
	}
	var frame claudeResultFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return Usage{}
	}
	return frame.usage()
}

func resolveModel(cfg BindingConfig, model Model) string {
	switch model {
	case ModelHaiku:
		if cfg.HaikuModel != "" {
			return cfg.HaikuModel
		}
	case ModelOpus:
		if cfg.OpusModel != "" {
			return cfg.OpusModel
		}
	default:
		if cfg.SonnetModel != "" {
			return cfg.SonnetModel
		}
	}
	if cfg.SonnetModel != "" {
		return cfg.SonnetModel
	}
	switch cfg.Type {
	case "", "claude":
		return string(model)
	default:
		return ""
	}
}

func resolveEffort(cfg BindingConfig, effort string) string {
	switch effort {
	case "low":
		if cfg.LowEffort != "" {
			return cfg.LowEffort
		}
	case "medium":
		if cfg.MediumEffort != "" {
			return cfg.MediumEffort
		}
	case "high":
		if cfg.HighEffort != "" {
			return cfg.HighEffort
		}
	case "max":
		if cfg.MaxEffort != "" {
			return cfg.MaxEffort
		}
	}
	return effort
}

func extractAssistantText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ""
	}
	var b strings.Builder
	for _, c := range msg.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// newResultFile creates an empty temp file at the given pattern and
// returns its path plus a cleanup func. Shared by every runner that needs
// a Ralph-owned ResultPath for agent.Options.
func newResultFile(pattern string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("provider: create result file: %w", err)
	}
	path = f.Name()
	_ = f.Close()
	return path, func() { _ = os.Remove(path) }, nil
}
