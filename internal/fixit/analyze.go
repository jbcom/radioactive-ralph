package fixit

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/session"
)

//go:embed prompts/advisor.tmpl
var advisorTemplateRaw string

// AnalyzeOptions feeds Analyze.
type AnalyzeOptions struct {
	Intent IntentSpec
	RC     RepoContext
	Scores []VariantScore

	// ClaudeBin overrides the default `claude` binary path. Tests use
	// the cassette replayer or the fake-claude double here.
	ClaudeBin string

	// WorkingDir is the cwd for the spawned subprocess. Defaults to the
	// repo root from RC.GitRoot.
	WorkingDir string

	// Model pins the tier for the planning subprocess. Empty defaults
	// to "opus" — the advisor runs infrequently (once per plan), so
	// the cost is bounded and the quality delta is meaningful.
	Model string

	// Effort pins the reasoning-effort level. Empty defaults to "high"
	// so opus scales up on genuinely hard repos without burning tokens
	// on simple ones.
	Effort string

	// Timeout caps the total Claude analysis time. Default 180s —
	// opus with auto-effort can take longer than the old sonnet/medium
	// defaults, so the cap is lifted from 90s accordingly.
	Timeout time.Duration
}

// Analyze runs Stage 4. Returns a parsed PlanProposal on success, or
// (zero, error) on hard failure. Callers handle fallback emission;
// this function never returns a half-filled proposal.
//
// One retry on JSON-parse failure: when Claude returns text that
// doesn't unmarshal cleanly, we re-spawn with the parse error
// appended so the model can self-correct. Second failure bubbles up.
func Analyze(ctx context.Context, opts AnalyzeOptions) (PlanProposal, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 180 * time.Second
	}
	if opts.WorkingDir == "" {
		opts.WorkingDir = opts.RC.GitRoot
	}
	if opts.Model == "" {
		opts.Model = "opus"
	}
	if opts.Effort == "" {
		// `claude -p` accepts only low|medium|high|max (as of 2.1.108).
		// Default to high for opus so the planning subprocess reasons
		// deeply without operator tuning. Operators who want cheaper
		// runs pass --plan-effort medium or override in config.toml.
		opts.Effort = "high"
	}

	prompt, err := renderAdvisorPrompt(opts.Intent, opts.RC, opts.Scores)
	if err != nil {
		return PlanProposal{}, fmt.Errorf("render prompt: %w", err)
	}

	// Try once. On parse failure, retry once with the error appended.
	for attempt := 1; attempt <= 2; attempt++ {
		rawJSON, err := callClaude(ctx, opts, prompt)
		if err != nil {
			return PlanProposal{}, fmt.Errorf("attempt %d: claude call: %w", attempt, err)
		}
		proposal, perr := parseProposal(rawJSON)
		if perr == nil {
			return proposal, nil
		}
		if attempt == 2 {
			return PlanProposal{}, fmt.Errorf("attempt 2: parse: %w (raw output: %q)", perr, rawJSON)
		}
		// Append the parse error and the offending output to the prompt
		// for the retry.
		prompt = prompt + "\n\n# Previous attempt failed to parse\n\n" +
			"Your previous response could not be parsed. Error: " + perr.Error() + "\n\n" +
			"Previous output (truncated):\n" + truncate(rawJSON, 800) + "\n\n" +
			"Output ONLY the JSON object that matches the schema. No prose.\n"
	}
	return PlanProposal{}, errors.New("unreachable")
}

// renderAdvisorPrompt fills the embedded template with the live
// IntentSpec + RepoContext + scores.
func renderAdvisorPrompt(intent IntentSpec, rc RepoContext, scores []VariantScore) (string, error) {
	tmpl, err := template.New("advisor").Funcs(template.FuncMap{
		"join": strings.Join,
		"until": func(items []GitCommit, n int) []GitCommit {
			if len(items) > n {
				return items[:n]
			}
			return items
		},
	}).Parse(advisorTemplateRaw)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	data := map[string]any{
		"Topic":       intent.Topic,
		"Description": intent.Description,
		"Constraints": intent.Constraints,
		"RC":          rc,
		"Scores":      scores,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// callClaude spawns a sonnet subprocess in --advisor scope, sends the
// prompt as the system prompt + an empty trigger user message, and
// collects the assistant's text response. Returns the raw text
// (expected to be a JSON object).
func callClaude(ctx context.Context, opts AnalyzeOptions, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	s, err := session.Spawn(ctx, session.Options{
		ClaudeBin:    opts.ClaudeBin,
		WorkingDir:   opts.WorkingDir,
		SystemPrompt: prompt,
		Model:        opts.Model,
		Effort:       opts.Effort,
		// No tools — the advisor must produce text only.
		AllowedTools: []string{},
	})
	if err != nil {
		return "", fmt.Errorf("spawn: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Collect assistant text frames until result.
	var assistantBuf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		for ev := range s.Events() {
			if ev.Err != nil {
				done <- ev.Err
				return
			}
			if ev.Inbound.Type == "assistant" {
				if text := extractAssistantText(ev.Inbound.Message); text != "" {
					assistantBuf.WriteString(text)
				}
			}
			if ev.Inbound.Type == "result" {
				done <- nil
				return
			}
		}
		done <- errors.New("event stream closed without result")
	}()

	if err := s.SendUserMessage(ctx, "Produce the PlanProposal now."); err != nil {
		return "", fmt.Errorf("send: %w", err)
	}
	if err := <-done; err != nil {
		return "", err
	}
	return strings.TrimSpace(assistantBuf.String()), nil
}

// extractAssistantText pulls the concatenated text from an assistant
// frame's message field. The Claude content-block format wraps text
// in {role, content: [{type:"text", text:"..."}, ...]}.
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

// parseProposal decodes the JSON object Claude returned with strict
// unknown-field rejection.
func parseProposal(raw string) (PlanProposal, error) {
	// Be lenient about leading whitespace / fences in case Claude
	// drifts. Find the first '{' and the matching last '}'.
	openIdx := strings.Index(raw, "{")
	closeIdx := strings.LastIndex(raw, "}")
	if openIdx < 0 || closeIdx < 0 || closeIdx < openIdx {
		return PlanProposal{}, fmt.Errorf("no JSON object found in response")
	}
	body := raw[openIdx : closeIdx+1]
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var p PlanProposal
	if err := dec.Decode(&p); err != nil {
		return PlanProposal{}, err
	}
	return p, nil
}

// truncate returns s capped at n runes with an ellipsis suffix.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
