package runtime

import (
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

func TestRenderVariantSystemPromptIncludesVariantDirectives(t *testing.T) {
	p, _ := variant.Lookup("green")
	out := renderVariantSystemPrompt(p)
	if !strings.Contains(out, "green") {
		t.Errorf("expected variant name in prompt:\n%s", out)
	}
	if !strings.Contains(out, "steady forward progress") {
		t.Errorf("expected prompt directive in prompt:\n%s", out)
	}
}

func TestRenderVariantSystemPromptSkipsEmptyDirectives(t *testing.T) {
	p, _ := variant.Lookup("green")
	p.PromptDirectives = append([]string{"", "  ", "direct"}, p.PromptDirectives...)
	out := renderVariantSystemPrompt(p)
	if strings.Contains(out, "\n- \n") {
		t.Errorf("blank directive leaked into prompt:\n%s", out)
	}
	if !strings.Contains(out, "- direct") {
		t.Errorf("explicit directive missing from prompt:\n%s", out)
	}
}

func TestRenderVariantSystemPromptDeterministic(t *testing.T) {
	p, _ := variant.Lookup("green")
	a := renderVariantSystemPrompt(p)
	b := renderVariantSystemPrompt(p)
	if a != b {
		t.Errorf("prompt rendering not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}
