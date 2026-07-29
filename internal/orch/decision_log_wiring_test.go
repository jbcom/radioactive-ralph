package orch

import (
	"os"
	"strings"
	"testing"
)

// TestDecisionLogIsWiredIntoDispatch guards a subsystem that was fully built
// and never invoked.
//
// WriteWorkerDecision and AbsorbDecisionLog are implemented, documented, and
// covered by lifecycle_test.go -- and until this test, NOTHING in dispatch
// called either. AbsorbDecisionLog's own doc comment says callers "should call
// it once per worker lifecycle end (normal completion, verification rejection,
// or kill+reclaim)", and no such call existed.
//
// The cost was concrete, not theoretical. A self-test step failed
// `interactive_prompt` on two consecutive clean-tree runs, and there was no way
// to learn what the turn asked for: `messages` returns metadata only (content
// is withheld by the content-safety contract, correctly), and no decision log
// existed for any worker because none was ever written. The product could not
// answer its own most important diagnostic question.
//
// AGENTS.md already names this shape -- "a correct guard nothing calls is the
// same defect shape as containment shipping with zero callers". This asserts
// the wiring at the SOURCE, because a behavioural test would need a live
// provider turn, and the failure mode is precisely that a correct function is
// never reached.
func TestDecisionLogIsWiredIntoDispatch(t *testing.T) {
	src, err := os.ReadFile("orchestrator.go")
	if err != nil {
		t.Fatalf("read orchestrator.go: %v", err)
	}
	text := string(src)

	// Strip comments so a mention in prose cannot satisfy the check -- the
	// existing "decisionLogAbsorb" comment did exactly that, making the
	// subsystem look wired to a grep.
	var code strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}

	if !strings.Contains(code.String(), "AbsorbDecisionLog(") {
		t.Error("dispatch never calls AbsorbDecisionLog; a worker's decisions " +
			"are written to a file nothing ever reads, so a failed turn leaves " +
			"no readable record of what it decided")
	}
}
