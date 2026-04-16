package variant

import (
	"strings"
	"testing"
)

// ── Per-profile parametrized invariants ------------------------------

// TestProfileValidates ensures every profile is internally consistent.
func TestProfileValidates(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if err := p.Validate(); err != nil {
				t.Errorf("%s.Validate(): %v", name, err)
			}
		})
	}
}

// TestProfileHasDescription — every variant needs a one-liner for
// `radioactive_ralph init` and `--help`. Blank descriptions are a bug.
func TestProfileHasDescription(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if strings.TrimSpace(p.Description) == "" {
				t.Errorf("%s: Description is empty", name)
			}
		})
	}
}

func TestProfileDeclaresExecutionMode(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if !p.AttachedAllowed && !p.DurableAllowed {
				t.Errorf("%s must allow at least one execution mode", name)
			}
		})
	}
}

// TestProfileToolAllowlistOnlyContainsKnownTools — typos in tool names
// would silently neuter a variant, so guard with a whitelist.
func TestProfileToolAllowlistOnlyContainsKnownTools(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			for _, tool := range p.ToolAllowlist {
				if _, ok := knownTools[tool]; !ok {
					t.Errorf("%s: unknown tool %q in allowlist", name, tool)
				}
			}
		})
	}
}

// TestProfileModelsAreValid — stage → model map uses only the three
// known model tiers.
func TestProfileModelsAreValid(t *testing.T) {
	validModels := map[Model]bool{ModelHaiku: true, ModelSonnet: true, ModelOpus: true}
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if len(p.Models) == 0 {
				t.Errorf("%s: Models map is empty", name)
			}
			for stage, model := range p.Models {
				if !validModels[model] {
					t.Errorf("%s: stage %q maps to invalid model %q", name, stage, model)
				}
			}
		})
	}
}

// ── Variant-specific spec invariants ---------------------------------

// Blue is structurally read-only: no Edit, no Write.
func TestBlueExcludesWrites(t *testing.T) {
	p := mustLookup(t, "blue")
	if p.WritesAllowed() {
		t.Error("blue must not permit Edit or Write")
	}
	for _, tool := range p.ToolAllowlist {
		if tool == ToolEdit || tool == ToolWrite {
			t.Errorf("blue has %q in allowlist", tool)
		}
	}
	if p.Isolation != IsolationShared {
		t.Errorf("blue.Isolation = %q, want shared", p.Isolation)
	}
}

// Grey is single-pass, single-worktree, haiku-only.
func TestGreyIsSinglePassHaikuOnly(t *testing.T) {
	p := mustLookup(t, "grey")
	if p.Termination != TerminationSinglePass {
		t.Errorf("grey.Termination = %q, want single-pass", p.Termination)
	}
	if p.MaxParallelWorktrees != 1 {
		t.Errorf("grey.MaxParallelWorktrees = %d, want 1", p.MaxParallelWorktrees)
	}
	for stage, model := range p.Models {
		if model != ModelHaiku {
			t.Errorf("grey stage %q = %q, want haiku (grey is haiku-exclusive)", stage, model)
		}
	}
}

// Green — infinite loop, mirror-pool, sonnet default.
func TestGreenIsInfiniteMirrorPool(t *testing.T) {
	p := mustLookup(t, "green")
	if p.Termination != TerminationInfinite {
		t.Errorf("green.Termination = %q, want infinite", p.Termination)
	}
	if p.Isolation != IsolationMirrorPool {
		t.Errorf("green.Isolation = %q, want mirror-pool", p.Isolation)
	}
	if p.MaxParallelWorktrees != 6 {
		t.Errorf("green.MaxParallelWorktrees = %d, want 6 (per SKILL.md)", p.MaxParallelWorktrees)
	}
	if p.ModelForStage(StageExecute) != ModelSonnet {
		t.Error("green execute stage should be sonnet")
	}
}

// Red — single-pass triage with up to 8 parallel agents.
func TestRedIsSinglePassTriage(t *testing.T) {
	p := mustLookup(t, "red")
	if p.Termination != TerminationSinglePass {
		t.Errorf("red.Termination = %q, want single-pass", p.Termination)
	}
	if p.MaxParallelWorktrees != 8 {
		t.Errorf("red.MaxParallelWorktrees = %d, want 8 (triage speed)", p.MaxParallelWorktrees)
	}
}

// Professor — planning stage uses opus, up to 4 parallel execution.
func TestProfessorPlansWithOpus(t *testing.T) {
	p := mustLookup(t, "professor")
	if p.ModelForStage(StagePlan) != ModelOpus {
		t.Error("professor plan stage should be opus")
	}
	if p.ModelForStage(StageExecute) != ModelSonnet {
		t.Error("professor execute stage should be sonnet")
	}
	if p.MaxParallelWorktrees != 4 {
		t.Errorf("professor.MaxParallelWorktrees = %d, want 4", p.MaxParallelWorktrees)
	}
}

// Fixit — N-cycles with spend cap, single repo, sonnet default.
func TestFixitIsNCyclesWithSpendCap(t *testing.T) {
	p := mustLookup(t, "fixit")
	if p.Termination != TerminationNCycles {
		t.Errorf("fixit.Termination = %q, want n-cycles", p.Termination)
	}
	if p.CycleLimit <= 0 {
		t.Errorf("fixit.CycleLimit = %d, want > 0", p.CycleLimit)
	}
	if !p.SafetyFloors.RequireSpendCap {
		t.Error("fixit must require spend cap")
	}
	if p.MaxParallelWorktrees != 1 {
		t.Errorf("fixit.MaxParallelWorktrees = %d, want 1 (single repo)", p.MaxParallelWorktrees)
	}
}

// Immortal — infinite loop, sonnet only, limited parallelism for resilience.
func TestImmortalIsSonnetOnlyInfinite(t *testing.T) {
	p := mustLookup(t, "immortal")
	if p.Termination != TerminationInfinite {
		t.Errorf("immortal.Termination = %q, want infinite", p.Termination)
	}
	for stage, model := range p.Models {
		if model != ModelSonnet {
			t.Errorf("immortal stage %q = %q, want sonnet (immortal is sonnet-only)", stage, model)
		}
	}
	if p.MaxParallelWorktrees > 3 {
		t.Errorf("immortal.MaxParallelWorktrees = %d, want ≤3 (resilience > speed)", p.MaxParallelWorktrees)
	}
}

// ── Gated variants share floor invariants ----------------------------

var gatedVariants = []Name{Savage, OldMan, WorldBreaker}

func TestGatedVariantsDeclareConfirmationGate(t *testing.T) {
	for _, name := range gatedVariants {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if !p.HasGate() {
				t.Errorf("%s must declare a ConfirmationGate", name)
			}
			if !strings.HasPrefix(p.ConfirmationGate, "--") {
				t.Errorf("%s.ConfirmationGate = %q, want ---prefixed CLI flag", name, p.ConfirmationGate)
			}
			if !p.SafetyFloors.FreshConfirmPerInvocation {
				t.Errorf("%s must require fresh confirmation per invocation", name)
			}
		})
	}
}

func TestInfiniteVariantsRequireDurableRuntime(t *testing.T) {
	for _, name := range []Name{Green, Professor, Immortal, Savage, WorldBreaker} {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if p.AttachedAllowed {
				t.Errorf("%s should require the durable repo service for execution", name)
			}
			if !p.DurableAllowed {
				t.Errorf("%s must remain available in durable mode", name)
			}
		})
	}
}

func TestBoundedVariantsAllowAttachedExecution(t *testing.T) {
	for _, name := range []Name{Blue, Grey, Red, Fixit, OldMan} {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if !p.AttachedAllowed {
				t.Errorf("%s should allow attached/headless execution", name)
			}
		})
	}
}

// Destructive variants (old-man, world-breaker) pin object_store=full.
func TestDestructiveVariantsPinObjectStoreFull(t *testing.T) {
	for _, name := range []Name{OldMan, WorldBreaker} {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if p.SafetyFloors.ObjectStore != ObjectStoreFull {
				t.Errorf("%s.SafetyFloors.ObjectStore = %q, want full", name, p.SafetyFloors.ObjectStore)
			}
			if p.ObjectStoreDefault != ObjectStoreFull {
				t.Errorf("%s.ObjectStoreDefault = %q, want full", name, p.ObjectStoreDefault)
			}
		})
	}
}

// Old-Man refuses default branches. World-Breaker does not (it's destructive
// to budget, not branch state).
func TestOldManRefusesDefaultBranch(t *testing.T) {
	p := mustLookup(t, "old-man")
	if !p.SafetyFloors.RefuseDefaultBranch {
		t.Error("old-man must refuse default branches")
	}
}

// Savage and World-Breaker require spend caps.
func TestBudgetBurnersRequireSpendCap(t *testing.T) {
	for _, name := range []Name{Savage, WorldBreaker} {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if !p.SafetyFloors.RequireSpendCap {
				t.Errorf("%s must require spend cap", name)
			}
		})
	}
}
