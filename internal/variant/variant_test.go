package variant

import (
	"errors"
	"strings"
	"testing"
)

// Known-good tool set — anything outside this is a typo in a profile.
var knownTools = map[string]struct{}{
	ToolAgent:      {},
	ToolBash:       {},
	ToolEdit:       {},
	ToolGlob:       {},
	ToolGrep:       {},
	ToolRead:       {},
	ToolWrite:      {},
	ToolTaskCreate: {},
	ToolTaskUpdate: {},
	ToolTaskList:   {},
}

// All ten variant names — the parametrized tests walk this list.
var allVariantNames = []Name{
	Blue, Grey, Green, Red, Professor, Fixit,
	Immortal, Savage, OldMan, WorldBreaker,
}

// ── Registry & lookup -------------------------------------------------

func TestLookupAllTenVariants(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p, err := Lookup(string(name))
			if err != nil {
				t.Fatalf("Lookup(%q): %v", name, err)
			}
			if p.Name != name {
				t.Errorf("Lookup(%q).Name = %q", name, p.Name)
			}
		})
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	for _, form := range []string{"green", "Green", "  GREEN  ", "GrEeN"} {
		if _, err := Lookup(form); err != nil {
			t.Errorf("Lookup(%q): %v", form, err)
		}
	}
}

func TestLookupNotFound(t *testing.T) {
	_, err := Lookup("purple")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAllReturnsAllTenProfiles(t *testing.T) {
	list := All()
	if len(list) != 10 {
		t.Fatalf("All() returned %d profiles, want 10", len(list))
	}
	seen := make(map[Name]bool)
	for _, p := range list {
		seen[p.Name] = true
	}
	for _, name := range allVariantNames {
		if !seen[name] {
			t.Errorf("variant %q missing from All()", name)
		}
	}
}

// ── Per-profile invariants --------------------------------------------

// TestProfileValidates ensures every profile is internally consistent.
func TestProfileValidates(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p, err := Lookup(string(name))
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if err := p.Validate(); err != nil {
				t.Errorf("%s.Validate(): %v", name, err)
			}
		})
	}
}

// TestProfileHasDescription — every variant needs a one-liner for
// `ralph init` and `--help`. Blank descriptions are a bug.
func TestProfileHasDescription(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p, _ := Lookup(string(name))
			if strings.TrimSpace(p.Description) == "" {
				t.Errorf("%s: Description is empty", name)
			}
		})
	}
}

// TestProfileToolAllowlistOnlyContainsKnownTools — typos in tool names
// would silently neuter a variant, so guard with a whitelist.
func TestProfileToolAllowlistOnlyContainsKnownTools(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p, _ := Lookup(string(name))
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
			p, _ := Lookup(string(name))
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

// ── Variant-specific spec invariants ----------------------------------

// Blue is structurally read-only: no Edit, no Write.
func TestBlueExcludesWrites(t *testing.T) {
	p, _ := Lookup("blue")
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
	p, _ := Lookup("grey")
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
	p, _ := Lookup("green")
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
	p, _ := Lookup("red")
	if p.Termination != TerminationSinglePass {
		t.Errorf("red.Termination = %q, want single-pass", p.Termination)
	}
	if p.MaxParallelWorktrees != 8 {
		t.Errorf("red.MaxParallelWorktrees = %d, want 8 (triage speed)", p.MaxParallelWorktrees)
	}
}

// Professor — planning stage uses opus, up to 4 parallel execution.
func TestProfessorPlansWithOpus(t *testing.T) {
	p, _ := Lookup("professor")
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
	p, _ := Lookup("fixit")
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
	p, _ := Lookup("immortal")
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
			p, _ := Lookup(string(name))
			if !p.HasGate() {
				t.Errorf("%s must declare a ConfirmationGate", name)
			}
			if !strings.HasPrefix(p.ConfirmationGate, "--") {
				t.Errorf("%s.ConfirmationGate = %q, want ---prefixed CLI flag", name, p.ConfirmationGate)
			}
			if !p.SafetyFloors.FreshConfirmPerInvocation {
				t.Errorf("%s must require fresh confirmation per invocation", name)
			}
			if !p.SafetyFloors.RefuseServiceContext {
				t.Errorf("%s must refuse service (launchd/systemd) context", name)
			}
		})
	}
}

// Destructive variants (old-man, world-breaker) pin object_store=full.
func TestDestructiveVariantsPinObjectStoreFull(t *testing.T) {
	for _, name := range []Name{OldMan, WorldBreaker} {
		t.Run(string(name), func(t *testing.T) {
			p, _ := Lookup(string(name))
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
	p, _ := Lookup("old-man")
	if !p.SafetyFloors.RefuseDefaultBranch {
		t.Error("old-man must refuse default branches")
	}
}

// Savage and World-Breaker require spend caps.
func TestBudgetBurnersRequireSpendCap(t *testing.T) {
	for _, name := range []Name{Savage, WorldBreaker} {
		t.Run(string(name), func(t *testing.T) {
			p, _ := Lookup(string(name))
			if !p.SafetyFloors.RequireSpendCap {
				t.Errorf("%s must require spend cap", name)
			}
		})
	}
}

// ── Method surface tests ---------------------------------------------

func TestWritesAllowedForGreen(t *testing.T) {
	p, _ := Lookup("green")
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

// ── Validate failure modes -------------------------------------------

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
		ToolAllowlist: []string{ToolBash, ToolEdit},
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for shared + Edit")
	}
	if !strings.Contains(err.Error(), "shared isolation forbids") {
		t.Errorf("error should mention shared/Edit: %v", err)
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

// ── Registry management ----------------------------------------------

func TestRegisterInvalidProfileReturnsError(t *testing.T) {
	err := Register(Profile{
		Name:          "x",
		Isolation:     IsolationShared,
		ToolAllowlist: []string{ToolWrite},
	})
	if err == nil {
		t.Fatal("expected error for invalid profile")
	}
}

func TestMustRegisterPanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	MustRegister(Profile{})
}

func TestResetRegistry(t *testing.T) {
	extra := Profile{
		Name:          "extra",
		Isolation:     IsolationShared,
		ToolAllowlist: []string{ToolRead},
	}
	if err := Register(extra); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := Lookup("extra"); err != nil {
		t.Errorf("extra not found after register: %v", err)
	}
	ResetRegistryForTesting()
	if _, err := Lookup("extra"); err == nil {
		t.Error("extra should be gone after reset")
	}
	// All ten built-ins survive reset.
	for _, name := range allVariantNames {
		if _, err := Lookup(string(name)); err != nil {
			t.Errorf("built-in %q should survive reset: %v", name, err)
		}
	}
}
