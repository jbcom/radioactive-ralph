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

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

//go:embed prompts/advisor.tmpl
var advisorTemplateRaw string

// AnalyzeOptions feeds Analyze.
type AnalyzeOptions struct {
	Intent IntentSpec
	RC     RepoContext
	Scores []VariantScore

	// Binding is the provider binding used for stage-4 planning.
	// Zero value falls back to the built-in `claude` binding.
	Binding provider.Binding

	// ProviderBinary overrides the resolved provider binary. Tests may
	// point this at a fake CLI.
	ProviderBinary string

	// RunnerFactory constructs the provider runner. Nil defaults to
	// provider.NewRunner.
	RunnerFactory func(binding provider.Binding) (provider.Runner, error)

	// WorkingDir is the cwd for the spawned subprocess. Defaults to the
	// repo root from RC.GitRoot.
	WorkingDir string

	// Model pins the planning tier. Empty defaults to "opus" — the
	// advisor runs infrequently, so the cost is bounded and the
	// quality delta is meaningful.
	Model string

	// Effort pins the reasoning-effort level. Empty defaults to "high"
	// so opus scales up on genuinely hard repos without burning tokens
	// on simple ones.
	Effort string

	// Timeout caps the total provider analysis time. Default 180s.
	Timeout time.Duration
}

// Analyze runs Stage 4. Returns a parsed PlanProposal on success, or
// (zero, error) on hard failure. Callers handle fallback emission;
// this function never returns a half-filled proposal.
//
// One retry on JSON-parse failure: when the provider returns text that
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
		opts.Effort = "high"
	}

	prompt, err := renderAdvisorPrompt(opts.Intent, opts.RC, opts.Scores)
	if err != nil {
		return PlanProposal{}, fmt.Errorf("render prompt: %w", err)
	}

	// Try once. On parse failure, retry once with the error appended.
	for attempt := 1; attempt <= 2; attempt++ {
		rawJSON, err := callProvider(ctx, opts, prompt)
		if err != nil {
			return PlanProposal{}, fmt.Errorf("attempt %d: provider call: %w", attempt, err)
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

// callProvider executes one planning turn through the configured
// provider binding and returns the raw assistant text expected to
// contain a single JSON object.
func callProvider(ctx context.Context, opts AnalyzeOptions, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	binding := opts.Binding
	if binding.Name == "" {
		binding = provider.Binding{Name: "claude", Config: config.DefaultClaudeProvider()}
	}
	if binding.Config.Type == "" {
		binding.Config.Type = binding.Name
	}
	if binding.Config.Binary == "" {
		binding.Config.Binary = builtInBinary(binding.Config.Type)
	}
	if opts.ProviderBinary != "" {
		binding.Config.Binary = opts.ProviderBinary
	}
	model, err := parsePlanningModel(opts.Model)
	if err != nil {
		return "", err
	}
	runnerFactory := opts.RunnerFactory
	if runnerFactory == nil {
		runnerFactory = provider.NewRunner
	}
	runner, err := runnerFactory(binding)
	if err != nil {
		return "", fmt.Errorf("runner: %w", err)
	}
	result, err := runner.Run(ctx, binding, provider.Request{
		WorkingDir:   opts.WorkingDir,
		SystemPrompt: prompt,
		UserPrompt:   "Produce the PlanProposal now.",
		OutputSchema: proposalSchema(),
		Model:        model,
		Effort:       opts.Effort,
		AllowedTools: []string{},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.AssistantOutput), nil
}

// parseProposal decodes the JSON object the provider returned with strict
// unknown-field rejection.
func parseProposal(raw string) (PlanProposal, error) {
	// Be lenient about leading whitespace / fences in case the provider
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

func parsePlanningModel(raw string) (variant.Model, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(variant.ModelOpus):
		return variant.ModelOpus, nil
	case string(variant.ModelSonnet):
		return variant.ModelSonnet, nil
	case string(variant.ModelHaiku):
		return variant.ModelHaiku, nil
	default:
		return "", fmt.Errorf("unsupported planning model %q; use haiku, sonnet, or opus", raw)
	}
}

func builtInBinary(providerType string) string {
	switch providerType {
	case "codex":
		return "codex"
	case "gemini":
		return "gemini"
	default:
		return "claude"
	}
}

func proposalSchema() string {
	return `{
  "type": "object",
  "additionalProperties": false,
  "required": ["primary", "primary_rationale", "tasks", "acceptance_criteria", "confidence"],
  "properties": {
    "primary": {"type": "string"},
    "primary_rationale": {"type": "string"},
    "alternate": {"type": "string"},
    "alternate_when": {"type": "string"},
    "confidence": {"type": "integer"},
    "acceptance_criteria": {
      "type": "array",
      "items": {"type": "string"}
    },
    "tasks": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["title", "effort", "impact"],
        "properties": {
          "title": {"type": "string"},
          "effort": {"type": "string"},
          "impact": {"type": "string"},
          "variant_hint": {"type": "string"},
          "context_boundary": {"type": "boolean"},
          "acceptance_criteria": {
            "type": "array",
            "items": {"type": "string"}
          },
          "depends_on": {
            "type": "array",
            "items": {"type": "string"}
          }
        }
      }
    }
  }
}`
}
