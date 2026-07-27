package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// ClaudeRunner executes a single `claude -p` turn under Ralph's own pty via
// internal/agent, per spec §2/§3: Ralph owns the pty-backed output
// (agent.Start), the pane/output stream is for human/watchdog observation,
// and the structured stream is framed before being accepted as result data.
//
// claude has no native "write result to a file" flag (verified against
// `claude --help` on the installed 2.1.218 CLI: --output-format
// json/stream-json both write to stdout only). So the ResultPath file here
// is Ralph-side, not CLI-native: the runner tees every stdout line (which
// IS the stream-json frames — the same content a human pane would show)
// into req's bounded ResultPath evidence file while parsing the same bounded
// frames for assistant text and the terminal result. This keeps the
// "never scrape the rendered pane for data" invariant: ResultPath holds
// the same raw JSON lines the CLI emitted, not a re-rendered terminal.
type ClaudeRunner struct{}

// ErrClaudeResultFailed is the static failure for is_error results and
// non-success subtypes not assigned a narrower category.
var ErrClaudeResultFailed = errors.New("provider: claude reported an unsuccessful result")

// ErrClaudeMaximumTurns is the static category for error_max_turns.
var ErrClaudeMaximumTurns = errors.New("provider: claude maximum-turn limit reached")

// ErrClaudeMissingResult means Claude exited cleanly without its required
// authoritative result frame.
var ErrClaudeMissingResult = errors.New("provider: claude exited without a result frame")

// Narrower failure categories, derived from the result frame's
// api_error_status. Each is a distinct remediation an operator can act on:
// re-authenticate, wait, change model, or retry. Before these existed every one
// of them arrived as ErrClaudeResultFailed — "claude reported an unsuccessful
// result" — which tells an operator nothing.
//
// Each is a FIXED CONSTANT. No text from the provider crosses this boundary:
// the frame's `result` field carries operator-facing prose from an external
// process ("Invalid API key · Fix external API key"), and laundering that into
// Ralph's error surface is exactly what the never-scrape invariant forbids.
var (
	// ErrClaudeAuthentication is a rejected credential (HTTP 401).
	ErrClaudeAuthentication = errors.New("provider: claude authentication rejected")
	// ErrClaudeModelAccess is a forbidden model or account (HTTP 403).
	ErrClaudeModelAccess = errors.New("provider: claude model unavailable or access denied")
	// ErrClaudeRateLimit is throttling (HTTP 429).
	ErrClaudeRateLimit = errors.New("provider: claude rate limited")
	// ErrClaudeServiceUnavailable is an upstream fault (HTTP 5xx).
	ErrClaudeServiceUnavailable = errors.New("provider: claude service unavailable")
	// ErrClaudeInvalidRequest is a malformed or rejected request (HTTP 400).
	ErrClaudeInvalidRequest = errors.New("provider: claude rejected the request")
)

// ClaudeFailureRetryable reports whether a claude failure category is worth
// retrying.
//
// The split matters operationally. Retrying an invalid credential burns the
// retry budget and DELAYS the operator seeing the real problem, because a key
// does not fix itself; a 429 or a 503, by contrast, is precisely what retries
// are for.
func ClaudeFailureRetryable(err error) bool {
	switch {
	case errors.Is(err, ErrClaudeRateLimit),
		errors.Is(err, ErrClaudeServiceUnavailable):
		return true
	default:
		return false
	}
}

// Run spawns `claude -p --input-format stream-json --output-format
// stream-json` under agent.Start, feeds req.UserPrompt through a finite stdin
// pipe (claude in --input-format stream-json mode reads a JSON-line user
// message from stdin), tees stdout into a bounded ResultPath evidence file,
// and parses the terminal result frame for Usage.
func (ClaudeRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	limits, err := ResolveTurnLimits(binding, req)
	if err != nil {
		return Result{}, err
	}
	ctx, cancelTurn := WithTurnDeadline(ctx, limits.TurnTimeout)
	defer cancelTurn()

	model := resolveModel(binding.Config, req.Model)
	effort := resolveEffort(binding.Config, req.Effort)
	input, err := streamJSONInput(req.UserPrompt)
	if err != nil {
		return Result{}, fmt.Errorf("provider: encode claude input: %w", err)
	}

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
		// Bound every PTY byte, including partial/oversized/non-JSON records
		// that never reach the structured callback.
		MaxObservedOutputBytes: maxStructuredEvidenceBytes,
		// claude is driven over stdin (stream-json). Disable pty echo so our
		// own prompt text isn't reflected back and pattern-matched by the
		// watchdog as an interactive prompt (which would kill the turn).
		DisableEcho: true,
		// Claude's one-turn stream-json protocol exits on stdin EOF. A finite
		// pipe provides that EOF without closing the PTY output channel, so
		// Ralph can still observe natural exit and preserve a later nonzero
		// process status.
		OneShotInput: input,
	}
	a, err := agent.Start(ctx, opts)
	if err != nil {
		return Result{}, fmt.Errorf("provider: start claude agent: %w", err)
	}

	resultFile, err := newBoundedEvidenceFile(resultPath)
	if err != nil {
		return Result{}, errors.Join(
			fmt.Errorf("provider: create result file: %w", err),
			a.TerminateAndWait(),
		)
	}

	var assistant boundedResultBuffer
	var sawResult bool
	var frame claudeResultFrame
	var ingestErr error
	var resultErr error

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
		if err := resultFile.writeFrame(line); err != nil {
			ingestErr = err
			return true
		}

		if text != "" {
			if err := assistant.writeString(text); err != nil {
				ingestErr = err
				return true
			}
		}
		if isResult {
			sawResult = true
			frame = f
			resultErr = f.failure()
			// A successful result frame is only a candidate success. Let
			// Claude exit naturally so a subsequent nonzero process status
			// cannot be laundered by an earlier success frame. An
			// authoritative failure frame may terminate immediately because
			// no later process status can turn that failed result into
			// success.
			return resultErr != nil
		}
		return false
	}

	runErr := superviseAgent(ctx, a, streamJSONWatchdogConfigWithStall(limits.StallTimeout), onLine)
	closeErr := resultFile.close()
	if runErr != nil {
		runErr = fmt.Errorf("provider: claude run: %w", runErr)
		if errors.Is(runErr, agent.ErrObservedOutputTooLarge) {
			// For a structured-stream provider the raw PTY ceiling is also an
			// upper bound on evidence. Preserve both sentinels: callers that
			// handled the v6 evidence ceiling remain compatible while the raw
			// transport cause stays inspectable.
			runErr = errors.Join(runErr, ErrStructuredEvidenceTooLarge)
		}
	}
	if ingestErr != nil || resultErr != nil {
		return Result{}, errors.Join(ingestErr, resultErr, runErr, closeErr)
	}
	if runErr != nil || closeErr != nil {
		return Result{}, errors.Join(runErr, closeErr)
	}
	if exitErr := a.ExitErr(); exitErr != nil {
		return Result{}, fmt.Errorf("provider: claude exited nonzero: %w", exitErr)
	}
	if !sawResult {
		return Result{}, ErrClaudeMissingResult
	}

	return Result{
		SessionID:       sessionID,
		AssistantOutput: normalizeStructuredOutput(assistant.String(), req),
		Usage:           frame.usage(),
	}, nil
}

// streamJSONInput encodes one Outbound-shaped user message as a JSON line,
// matching what claudesession.Session sends over a direct pipe today.
func streamJSONInput(userPrompt string) ([]byte, error) {
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
		return nil, err
	}
	return append(b, '\n'), nil
}

// claudeResultFrame is the terminal `type=result` stream-json frame.
type claudeResultFrame struct {
	TotalCostUSD float64 `json:"total_cost_usd"`
	Subtype      string  `json:"subtype"`
	IsError      bool    `json:"is_error"`
	// APIErrorStatus is the upstream HTTP status when the turn failed against
	// the API. It is the STRUCTURED failure signal — preferred over matching
	// the frame's human-readable `result` prose, which is unstable across CLI
	// versions and would drag provider text across the boundary.
	APIErrorStatus int `json:"api_error_status"`
	Usage          struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

func (f claudeResultFrame) failure() error {
	if !f.IsError && f.Subtype == "success" {
		return nil
	}
	if f.Subtype == "error_max_turns" {
		return ErrClaudeMaximumTurns
	}
	// Verified against claude 2.1.220: an invalid API key produces
	// {"is_error":true,"subtype":"success","api_error_status":401,...}. The
	// subtype says "success" on a hard failure, so subtype alone cannot be
	// trusted to categorize — and is_error above is what makes this a failure
	// at all. api_error_status is the reliable discriminator.
	if category := claudeFailureForStatus(f.APIErrorStatus); category != nil {
		return category
	}
	return ErrClaudeResultFailed
}

// claudeFailureForStatus maps an upstream HTTP status to a narrower category,
// or nil when the status is absent or unrecognized.
//
// Unrecognized stays GENERIC on purpose. Forcing an unknown status into the
// nearest category would send an operator to fix the wrong thing, which is
// worse than telling them only that the turn failed.
func claudeFailureForStatus(status int) error {
	switch {
	case status == 401:
		return ErrClaudeAuthentication
	case status == 403:
		return ErrClaudeModelAccess
	case status == 429:
		return ErrClaudeRateLimit
	case status == 400:
		return ErrClaudeInvalidRequest
	case status >= 500 && status <= 599:
		return ErrClaudeServiceUnavailable
	default:
		return nil
	}
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
// (banner text, terminal noise, blank lines) — callers use kind=="" to discard
// pane noise before it reaches a structured-result sink. For a
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
	}
	// Sonnet is the default tier: used for ModelSonnet AND as the fallback
	// when the requested tier has no configured override.
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
