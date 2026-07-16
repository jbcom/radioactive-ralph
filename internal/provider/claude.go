package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	claudesession "github.com/jbcom/radioactive-ralph/internal/provider/claudesession"
)

// ClaudeRunner executes a single `claude -p` turn.
type ClaudeRunner struct{}

// Run shells out to the configured Claude CLI binding and returns the
// assistant text that accumulated before the result frame.
func (ClaudeRunner) Run(ctx context.Context, binding Binding, req Request) (Result, error) {
	model := resolveModel(binding.Config, req.Model)
	effort := resolveEffort(binding.Config, req.Effort)

	opts := claudesession.Options{
		ClaudeBin:    binding.Config.Binary,
		WorkingDir:   req.WorkingDir,
		SystemPrompt: req.SystemPrompt,
		Model:        model,
		Effort:       effort,
		AllowedTools: req.AllowedTools,
		ExtraArgs:    binding.Config.Args,
	}
	s, err := claudesession.Spawn(ctx, opts)
	if err != nil {
		return Result{}, fmt.Errorf("spawn claude runner: %w", err)
	}
	defer func() { _ = s.Close() }()

	var assistant bytes.Buffer
	var usage Usage
	done := make(chan error, 1)
	go func() {
		for ev := range s.Events() {
			if ev.Err != nil {
				done <- ev.Err
				return
			}
			switch ev.Inbound.Type {
			case "assistant":
				if text := extractAssistantText(ev.Inbound.Message); text != "" {
					assistant.WriteString(text)
				}
			case "result":
				usage = parseClaudeUsage(ev.Inbound.Raw)
				done <- nil
				return
			}
		}
		done <- fmt.Errorf("provider: claude event stream closed without result")
	}()

	if err := s.SendUserMessage(ctx, req.UserPrompt); err != nil {
		return Result{}, fmt.Errorf("send claude request: %w", err)
	}
	if err := <-done; err != nil {
		return Result{}, err
	}
	return Result{
		SessionID:       s.SessionID(),
		AssistantOutput: normalizeStructuredOutput(assistant.String(), req),
		Usage:           usage,
	}, nil
}

// parseClaudeUsage extracts token/cost accounting from a stream-json
// `result` frame. Claude Code emits total_cost_usd and a usage object on
// the terminal result line; both are best-effort — a frame without them
// yields a zero Usage rather than an error.
func parseClaudeUsage(raw []byte) Usage {
	if len(raw) == 0 {
		return Usage{}
	}
	var frame struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
		Usage        struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return Usage{}
	}
	return Usage{
		InputTokens:       frame.Usage.InputTokens,
		OutputTokens:      frame.Usage.OutputTokens,
		CachedInputTokens: frame.Usage.CacheReadInputTokens + frame.Usage.CacheCreationInputTokens,
		CostUSD:           frame.TotalCostUSD,
	}
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
