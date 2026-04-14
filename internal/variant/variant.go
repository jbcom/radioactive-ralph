// Package variant defines the ten Ralph behavior profiles.
//
// Each variant is a distinct operating mode with its own parallelism,
// model tiering, commit cadence, termination policy, tool allowlist,
// safety floors, and skill-bias preferences. The Profile struct
// is the canonical source of truth the supervisor reads to decide
// how to spawn and manage Claude subprocesses.
//
// Each profile is registered from its own file (blue.go, grey.go, ...)
// so any operator can read the full definition of a single variant
// without wading through the others. Every profile is faithful to
// skills/<name>-ralph/SKILL.md.
package variant

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Name is the canonical variant identifier. Matches strings used in
// skills/<name>-ralph/SKILL.md frontmatter, CLI --variant flag, and
// voice.Variant.
type Name string

// The ten variants. Ordered from safest to most destructive, loosely
// matching the "hulk scale" documented in the PRD.
const (
	Blue         Name = "blue"          // read-only observer
	Grey         Name = "grey"          // mechanical single-pass hygiene
	Green        Name = "green"         // default workhorse
	Red          Name = "red"           // single-cycle incident response
	Professor    Name = "professor"     // plan → execute → reflect
	Fixit        Name = "fixit"         // advisor + ROI-scored N-cycle bursts; sole variant that recommends peers
	Immortal     Name = "immortal"      // multi-day resilient loop
	Savage       Name = "savage"        // max throughput, gated
	OldMan       Name = "old-man"       // forceful imposition, gated
	WorldBreaker Name = "world-breaker" // all-opus, gated
)

// IsolationMode controls where Ralph does its work relative to the
// operator's repo. See docs/reference/architecture.md for the
// full four-knob explanation.
type IsolationMode string

const (
	// IsolationShared runs against the operator's actual repo directory.
	// Only valid for variants that exclude Edit and Write from the tool
	// allowlist — enforced by Validate.
	IsolationShared IsolationMode = "shared"

	// IsolationShallow does a `git clone --depth=1` into XDG state.
	IsolationShallow IsolationMode = "shallow"

	// IsolationMirrorSingle is full `git clone --mirror` plus exactly one
	// worktree at a time.
	IsolationMirrorSingle IsolationMode = "mirror-single"

	// IsolationMirrorPool is a mirror clone plus N parallel worktrees.
	IsolationMirrorPool IsolationMode = "mirror-pool"
)

// ObjectStoreMode applies to the mirror-* isolation modes.
type ObjectStoreMode string

const (
	// ObjectStoreReference borrows objects from operator's repo via
	// --reference --dissociate=false. Fast clone, shared objects.
	ObjectStoreReference ObjectStoreMode = "reference"

	// ObjectStoreFull clones fully independently. Destructive variants
	// pin this — they cannot be overridden to Reference without an
	// explicit two-step operator confirmation.
	ObjectStoreFull ObjectStoreMode = "full"
)

// SyncSource controls where the mirror fetches from.
type SyncSource string

const (
	// SyncSourceLocal pulls from operator's local repo via file:// remote.
	SyncSourceLocal SyncSource = "local"

	// SyncSourceOrigin pulls from the real origin (GitHub/GitLab/...).
	SyncSourceOrigin SyncSource = "origin"

	// SyncSourceBoth configures both remotes; fetch from local, push to origin.
	SyncSourceBoth SyncSource = "both"
)

// LFSMode controls LFS handling.
type LFSMode string

const (
	// LFSFull clones LFS content normally.
	LFSFull LFSMode = "full"

	// LFSOnDemand fetches LFS content only when a task touches it.
	LFSOnDemand LFSMode = "on-demand"

	// LFSPointersOnly skips LFS blobs entirely.
	LFSPointersOnly LFSMode = "pointers-only"

	// LFSExcluded refuses tasks that touch LFS paths with a clear error.
	LFSExcluded LFSMode = "excluded"
)

// Stage identifies which phase of a variant's workflow a particular
// session spawn is for. Variants with a single stage (grey, fixit)
// always use StageExecute.
type Stage string

// Stages enumerate the phases a variant may spawn subprocesses for.
const (
	StagePlan    Stage = "plan"
	StageExecute Stage = "execute"
	StageReflect Stage = "reflect"
	StageReview  Stage = "review"
)

// Model identifies a specific Claude model tier.
type Model string

// Model tiers used by variant profiles.
const (
	ModelHaiku  Model = "haiku"
	ModelSonnet Model = "sonnet"
	ModelOpus   Model = "opus"
)

// TerminationPolicy classifies how a variant ends.
type TerminationPolicy string

// Termination policies — how a variant's loop concludes.
const (
	TerminationSinglePass TerminationPolicy = "single-pass" // one pass, exit
	TerminationNCycles    TerminationPolicy = "n-cycles"    // N cycles then exit with summary
	TerminationInfinite   TerminationPolicy = "infinite"    // runs until operator stops
)

// BiasCategory identifies a skill bias slot declared by the variant.
// Matches the keys the operator configures in [capabilities] in
// .radioactive-ralph/config.toml.
type BiasCategory string

// Bias categories that a variant may declare snippets for. Each maps
// to a [capabilities] key in config.toml.
const (
	BiasReview         BiasCategory = "review"
	BiasSecurityReview BiasCategory = "security_review"
	BiasDocsQuery      BiasCategory = "docs_query"
	BiasBrainstorm     BiasCategory = "brainstorm"
	BiasDebugging      BiasCategory = "debugging"
)

// BiasSnippet is the system-prompt injection used when a bias slot has
// a matching skill in the inventory.
//
// Placeholder: {skill} expands to the chosen skill's full name.
type BiasSnippet string

// Tool names that appear in allow/deny lists. Mirrors Claude Code's
// built-in tools.
const (
	ToolAgent      = "Agent"
	ToolBash       = "Bash"
	ToolEdit       = "Edit"
	ToolGlob       = "Glob"
	ToolGrep       = "Grep"
	ToolRead       = "Read"
	ToolWrite      = "Write"
	ToolTaskCreate = "TaskCreate"
	ToolTaskUpdate = "TaskUpdate"
	ToolTaskList   = "TaskList"
)

// SafetyFloors pins specific fields that cannot be weakened by
// config.toml, env vars, or single-flag CLI overrides.
type SafetyFloors struct {
	// ObjectStore pinned to a specific value. Zero string means
	// "no pin, respect config/defaults."
	ObjectStore ObjectStoreMode

	// RefuseDefaultBranch prevents runs on main/master/etc.
	RefuseDefaultBranch bool

	// FreshConfirmPerInvocation means the confirmation gate must be
	// passed on every single `ralph run` — never cached in config.
	FreshConfirmPerInvocation bool

	// RefuseServiceContext means the variant will not run when detected
	// under launchd/systemd (savage, old-man, world-breaker).
	RefuseServiceContext bool

	// RequireSpendCap means `ralph run` fails without a cap value
	// either via --spend-cap-usd flag or [variants.X] spend_cap_usd.
	RequireSpendCap bool
}

// Profile is the canonical description of a variant.
type Profile struct {
	Name                 Name
	Description          string // one-liner shown in `ralph --help` and `ralph init`
	Isolation            IsolationMode
	MaxParallelWorktrees int
	Models               map[Stage]Model // stage → model
	ToolAllowlist        []string        // Claude Code tool names
	Termination          TerminationPolicy
	CycleLimit           int    // meaningful only for NCycles
	ConfirmationGate     string // CLI flag like "--confirm-burn-budget"; empty = no gate
	ObjectStoreDefault   ObjectStoreMode
	SyncSourceDefault    SyncSource
	LFSModeDefault       LFSMode
	SafetyFloors         SafetyFloors
	SkillBiases          map[BiasCategory]BiasSnippet
}

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
	if p.Isolation == "" {
		return fmt.Errorf("variant %q: Isolation required", p.Name)
	}
	// Shared-isolation implies no writes — enforced here.
	if p.Isolation == IsolationShared && p.WritesAllowed() {
		return fmt.Errorf("variant %q: shared isolation forbids Edit/Write tools", p.Name)
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

// Registry -----------------------------------------------------------

var (
	regMu    sync.RWMutex
	registry = make(map[Name]Profile)
)

// Register adds profile to the global registry after validation.
// Intended to be called from variant package init functions.
// Returns an error rather than panicking so tests can exercise
// invalid profiles.
func Register(p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	regMu.Lock()
	defer regMu.Unlock()
	registry[p.Name] = p
	return nil
}

// MustRegister panics on validation failure. Used by built-in variant
// init functions where a validation error is a programmer bug.
func MustRegister(p Profile) {
	if err := Register(p); err != nil {
		panic(fmt.Sprintf("variant.MustRegister(%s): %v", p.Name, err))
	}
}

// Lookup returns the profile for name (case-insensitive match).
// Returns ErrNotFound if name isn't registered.
func Lookup(name string) (Profile, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	norm := Name(strings.ToLower(strings.TrimSpace(name)))
	if p, ok := registry[norm]; ok {
		return p, nil
	}
	return Profile{}, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// All returns every registered profile, sorted by Name.
func All() []Profile {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Profile, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// ErrNotFound indicates a Lookup for an unregistered variant.
var ErrNotFound = errors.New("variant: not found")

// ResetRegistryForTesting clears the registry and re-registers the
// built-in variants. Tests that mutate the registry call this from a
// t.Cleanup to avoid bleeding state between tests.
func ResetRegistryForTesting() {
	regMu.Lock()
	registry = make(map[Name]Profile)
	regMu.Unlock()
	// Re-register outside the lock — Register takes its own write lock,
	// and sync.RWMutex is not re-entrant.
	registerBuiltins()
}

func init() {
	registerBuiltins()
}

// registerBuiltins registers all ten variants. Each profile is defined
// in its own file (blue.go, grey.go, ...).
func registerBuiltins() {
	MustRegister(blueProfile())
	MustRegister(greyProfile())
	MustRegister(greenProfile())
	MustRegister(redProfile())
	MustRegister(professorProfile())
	MustRegister(fixitProfile())
	MustRegister(immortalProfile())
	MustRegister(savageProfile())
	MustRegister(oldManProfile())
	MustRegister(worldBreakerProfile())
}
