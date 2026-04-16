package variant

import (
	"errors"
	"fmt"
)

// WritesAllowed reports whether the variant's tool allowlist permits
// Edit or Write. Shared-isolation variants must return false.
func (p Profile) WritesAllowed() bool {
	for _, t := range p.ToolAllowlist {
		if t == ToolEdit || t == ToolWrite {
			return true
		}
	}
	return false
}

// CanMutateViaBash reports whether Bash is in the allowlist. Bash is
// strictly more powerful than Edit+Write — it can run `git commit`,
// `rm -rf`, curl+pipe-to-sh, and anything else. For shared-isolation
// variants, this is a backdoor around WritesAllowed unless the
// variant explicitly opts into it via ShellExplicitlyTrusted.
func (p Profile) CanMutateViaBash() bool {
	for _, t := range p.ToolAllowlist {
		if t == ToolBash {
			return true
		}
	}
	return false
}

// HasGate reports whether the variant requires a confirmation flag.
func (p Profile) HasGate() bool {
	return p.ConfirmationGate != ""
}

// Validate ensures the profile is internally consistent. Called by
// Register so new or edited variants fail fast with actionable errors.
func (p Profile) Validate() error {
	if p.Name == "" {
		return errors.New("variant: Name required")
	}
	if !p.AttachedAllowed && !p.DurableAllowed {
		return fmt.Errorf("variant %q: at least one execution mode must be allowed", p.Name)
	}
	if p.Isolation == "" {
		return fmt.Errorf("variant %q: Isolation required", p.Name)
	}
	// Shared-isolation implies no writes — enforced here.
	if p.Isolation == IsolationShared && p.WritesAllowed() {
		return fmt.Errorf("variant %q: shared isolation forbids Edit/Write tools", p.Name)
	}
	// Shared-isolation also forbids Bash unless the profile explicitly
	// opts in. Bash is a superset of Edit/Write via `git commit` and
	// arbitrary subprocess execution; permitting it under shared
	// isolation is a defense-in-depth violation without a trust
	// declaration. See Profile.ShellExplicitlyTrusted for rationale.
	if p.Isolation == IsolationShared && p.CanMutateViaBash() && !p.ShellExplicitlyTrusted {
		return fmt.Errorf(
			"variant %q: shared isolation forbids Bash unless ShellExplicitlyTrusted=true (Bash is strictly more powerful than Edit/Write)",
			p.Name)
	}
	// Mirror-pool needs >0 parallel worktrees.
	if p.Isolation == IsolationMirrorPool && p.MaxParallelWorktrees <= 0 {
		return fmt.Errorf("variant %q: mirror-pool requires MaxParallelWorktrees > 0", p.Name)
	}
	// Mirror-single is strict: exactly 1 worktree.
	if p.Isolation == IsolationMirrorSingle && p.MaxParallelWorktrees != 1 {
		return fmt.Errorf("variant %q: mirror-single requires MaxParallelWorktrees = 1 (got %d)",
			p.Name, p.MaxParallelWorktrees)
	}
	// N-cycles requires a positive limit.
	if p.Termination == TerminationNCycles && p.CycleLimit <= 0 {
		return fmt.Errorf("variant %q: n-cycles termination requires CycleLimit > 0", p.Name)
	}
	// Destructive variants must pin object_store = full.
	if p.SafetyFloors.ObjectStore != "" && p.ObjectStoreDefault != p.SafetyFloors.ObjectStore {
		return fmt.Errorf("variant %q: ObjectStoreDefault must match SafetyFloors.ObjectStore", p.Name)
	}
	return nil
}

// ModelForStage returns the model configured for the given stage, or
// the model for StageExecute if no specific stage is set. Returns
// ModelSonnet as the last-ditch default.
func (p Profile) ModelForStage(s Stage) Model {
	if m, ok := p.Models[s]; ok {
		return m
	}
	if m, ok := p.Models[StageExecute]; ok {
		return m
	}
	return ModelSonnet
}
