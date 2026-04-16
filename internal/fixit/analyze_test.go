package fixit

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

type stubRunner struct {
	output string
	err    error
}

func (s stubRunner) Run(_ context.Context, _ provider.Binding, _ provider.Request) (provider.Result, error) {
	if s.err != nil {
		return provider.Result{}, s.err
	}
	return provider.Result{AssistantOutput: s.output}, nil
}

func TestAnalyzeUsesProviderRunner(t *testing.T) {
	proposalJSON := `{
		"primary":"green",
		"primary_rationale":"The repo needs steady forward implementation.",
		"tasks":[{"title":"Implement the next bounded change","effort":"M","impact":"M"}],
		"acceptance_criteria":["CI passes", "Plan file exists", "Status returns healthy"],
		"confidence":81
	}`
	p, err := Analyze(context.Background(), AnalyzeOptions{
		Intent: IntentSpec{
			Topic:       "provider-test",
			Description: "exercise provider-backed planning",
		},
		RC: RepoContext{
			GitRoot:           "/tmp/repo",
			GovernanceMissing: []string{"CHANGELOG.md"},
			LangCounts:        map[string]int{".go": 12},
		},
		Scores: []VariantScore{{
			Variant: "green",
			Score:   80,
			Reasons: []string{"steady implementation path"},
		}},
		Binding: provider.Binding{
			Name: "codex",
		},
		RunnerFactory: func(binding provider.Binding) (provider.Runner, error) {
			if binding.Name != "codex" {
				t.Fatalf("binding.Name = %q, want codex", binding.Name)
			}
			return stubRunner{output: proposalJSON}, nil
		},
		Model:  "sonnet",
		Effort: "medium",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if p.Primary != "green" {
		t.Fatalf("Primary = %q, want green", p.Primary)
	}
	if p.Confidence != 81 {
		t.Fatalf("Confidence = %d, want 81", p.Confidence)
	}
}

func TestAnalyzeRejectsUnknownPlanningModel(t *testing.T) {
	_, err := Analyze(context.Background(), AnalyzeOptions{
		Intent: IntentSpec{Topic: "bad-model"},
		RC:     RepoContext{GitRoot: "/tmp/repo", LangCounts: map[string]int{}},
		Scores: []VariantScore{},
		RunnerFactory: func(_ provider.Binding) (provider.Runner, error) {
			t.Fatal("runner should not be constructed on invalid model")
			return nil, nil
		},
		Model: "gpt-5.4",
	})
	if err == nil {
		t.Fatal("expected invalid model error")
	}
}
