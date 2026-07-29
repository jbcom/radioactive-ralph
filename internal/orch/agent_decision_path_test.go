package orch

import (
	"os"
	"strings"
	"testing"
)

// TestScopedPromptOffersTheDecisionLog closes the producer half of a subsystem
// that is otherwise unreachable from the agent side.
//
// Ralph writes its OWN classification on failure ("turn ended
// interactive_prompt: ..."), and that is verified against a real run. But it
// records only THAT the provider asked for something -- never WHAT. The agent
// is the only party that knows, and it has no way to say: the scoped prompt
// carries plan, group, and step text, and no decision-log path or worker id.
//
// So WriteWorkerDecision has exactly one caller, Ralph itself, and the
// agent-authored side of the log has never been reachable. Telling the agent
// where to write is what makes the artifact answer the question it exists for.
func TestScopedPromptOffersTheDecisionLog(t *testing.T) {
	c := scopedContext{
		PlanTitle:       "Ship",
		GroupHeading:    "unit",
		StepText:        "run the orchestrator suite",
		DecisionLogPath: "/state/workers/worker-7.decisions.md",
	}
	got := c.prompt()

	if !strings.Contains(got, "/state/workers/worker-7.decisions.md") {
		t.Errorf("prompt = %q\n\nwant the decision-log path; without it the agent "+
			"cannot record why it blocked, and the operator surface can only ever "+
			"say THAT it blocked", got)
	}
	// The step text must survive: a prompt that buries the task under
	// instructions is worse than one that omits them.
	if !strings.Contains(got, "run the orchestrator suite") {
		t.Errorf("prompt = %q, want the step text intact", got)
	}

	// With no path configured, say nothing rather than emitting a dangling
	// instruction the agent cannot follow.
	bare := scopedContext{PlanTitle: "Ship", GroupHeading: "unit", StepText: "do it"}.prompt()
	if strings.Contains(bare, "decisions.md") || strings.Contains(strings.ToLower(bare), "decision log") {
		t.Errorf("prompt without a configured path = %q, want no decision-log "+
			"instruction at all", bare)
	}
}

// TestDispatchPopulatesTheDecisionLogPath is the half the prompt test cannot
// cover: a field nothing fills is the same defect as a function nothing calls,
// and this file exists because of exactly that pattern.
//
// Asserted at the SOURCE. A behavioural test needs a live provider turn, and
// the failure mode is precisely that a correct field is never populated -- so
// the check has to be that dispatch sets it.
func TestDispatchPopulatesTheDecisionLogPath(t *testing.T) {
	src, err := os.ReadFile("orchestrator.go")
	if err != nil {
		t.Fatalf("read orchestrator.go: %v", err)
	}
	var code strings.Builder
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	if !strings.Contains(code.String(), "DecisionLogPath: decisionPath") {
		t.Error("dispatch never populates scopedContext.DecisionLogPath; the " +
			"prompt would omit the instruction on every turn, leaving the " +
			"agent-authored decision log unreachable exactly as before")
	}
}
