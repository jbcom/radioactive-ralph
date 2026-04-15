package variant

import (
	"strings"
	"testing"
)

// ── Method-surface tests --------------------------------------------

func TestWritesAllowedForGreen(t *testing.T) {
	p := mustLookup(t, "green")
	if !p.WritesAllowed() {
		t.Error("green permits Edit + Write")
	}
}

func TestModelForStage(t *testing.T) {
	p := Profile{
		Name: "test",
		Models: map[Stage]Model{
			StagePlan:    ModelOpus,
			StageExecute: ModelSonnet,
		},
	}
	if p.ModelForStage(StagePlan) != ModelOpus {
		t.Error("plan stage should resolve to opus")
	}
	if p.ModelForStage(StageExecute) != ModelSonnet {
		t.Error("execute stage should resolve to sonnet")
	}
	if p.ModelForStage(StageReflect) != ModelSonnet {
		t.Error("unknown stage should fall back to execute's model")
	}
}

func TestModelForStageLastDitchDefault(t *testing.T) {
	p := Profile{Name: "test"}
	if p.ModelForStage(StageExecute) != ModelSonnet {
		t.Error("empty Models should return sensible default (sonnet)")
	}
}

func TestHasGate(t *testing.T) {
	cases := map[string]bool{
		"":                      false,
		"--confirm-burn-budget": true,
		"--anything":            true,
	}
	for gate, want := range cases {
		p := Profile{ConfirmationGate: gate}
		if got := p.HasGate(); got != want {
			t.Errorf("HasGate(gate=%q) = %v, want %v", gate, got, want)
		}
	}
}

// TestCanMutateViaBashDetection is a direct unit check on the helper
// so the shared-isolation security invariant has a test that points
// at the exact method the reviewer flagged.
func TestCanMutateViaBashDetection(t *testing.T) {
	cases := map[string]struct {
		tools []string
		want  bool
	}{
		"bash present":      {[]string{ToolBash, ToolRead}, true},
		"bash absent":       {[]string{ToolRead, ToolGrep}, false},
		"empty":             {nil, false},
		"edit is not bash":  {[]string{ToolEdit}, false},
		"write is not bash": {[]string{ToolWrite}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := Profile{ToolAllowlist: tc.tools}
			if got := p.CanMutateViaBash(); got != tc.want {
				t.Errorf("CanMutateViaBash(%v) = %v, want %v", tc.tools, got, tc.want)
			}
		})
	}
}

// ── Validate failure modes ------------------------------------------

func TestValidateRejectsMissingName(t *testing.T) {
	p := Profile{Isolation: IsolationShared}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing Name")
	}
}

func TestValidateRejectsSharedWithWrites(t *testing.T) {
	p := Profile{
		Name:          "bad",
		Isolation:     IsolationShared,
		ToolAllowlist: []string{ToolRead, ToolEdit},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for shared + Edit")
	}
	if !strings.Contains(err.Error(), "shared isolation forbids") {
		t.Errorf("error should mention shared/Edit: %v", err)
	}
}

// TestValidateRejectsSharedWithBashUntrusted ensures Bash under
// shared isolation requires an explicit ShellExplicitlyTrusted=true
// declaration. Bash is strictly more powerful than Edit/Write via
// `git commit` and arbitrary subprocess execution, so permitting it
// silently would be a defense-in-depth hole.
func TestValidateRejectsSharedWithBashUntrusted(t *testing.T) {
	p := Profile{
		Name:          "bad",
		Isolation:     IsolationShared,
		ToolAllowlist: []string{ToolBash, ToolRead},
		// ShellExplicitlyTrusted deliberately false.
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for shared + Bash without ShellExplicitlyTrusted")
	}
	if !strings.Contains(err.Error(), "ShellExplicitlyTrusted") {
		t.Errorf("error should mention ShellExplicitlyTrusted: %v", err)
	}
}

// TestValidateAcceptsSharedWithBashWhenExplicitlyTrusted confirms the
// opt-in path works — matches Blue's real configuration.
func TestValidateAcceptsSharedWithBashWhenExplicitlyTrusted(t *testing.T) {
	p := Profile{
		Name:                   "good",
		Isolation:              IsolationShared,
		ToolAllowlist:          []string{ToolBash, ToolRead},
		ShellExplicitlyTrusted: true,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("shared+Bash with ShellExplicitlyTrusted=true should validate: %v", err)
	}
}

func TestValidateRejectsMirrorPoolWithZeroParallel(t *testing.T) {
	p := Profile{
		Name:                 "bad",
		Isolation:            IsolationMirrorPool,
		MaxParallelWorktrees: 0,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for mirror-pool with 0 parallel")
	}
}

func TestValidateRejectsMirrorSingleWithWrongParallel(t *testing.T) {
	p := Profile{
		Name:                 "bad",
		Isolation:            IsolationMirrorSingle,
		MaxParallelWorktrees: 3,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for mirror-single with >1 parallel")
	}
}

func TestValidateRejectsNCyclesWithoutLimit(t *testing.T) {
	p := Profile{
		Name:                 "bad",
		Isolation:            IsolationMirrorSingle,
		MaxParallelWorktrees: 1,
		Termination:          TerminationNCycles,
		CycleLimit:           0,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error for n-cycles with no limit")
	}
}

func TestValidateRejectsFloorMismatch(t *testing.T) {
	p := Profile{
		Name:                 "bad",
		Isolation:            IsolationMirrorSingle,
		MaxParallelWorktrees: 1,
		ObjectStoreDefault:   ObjectStoreReference,
		SafetyFloors:         SafetyFloors{ObjectStore: ObjectStoreFull},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected error: default doesn't match floor")
	}
}
