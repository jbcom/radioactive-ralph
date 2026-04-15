// Package variant defines the ten Ralph behavior profiles.
//
// Each variant is a distinct operating mode with its own parallelism,
// model tiering, commit cadence, termination policy, tool allowlist,
// safety floors, and capability-bias preferences. The Profile struct
// is the canonical source of truth the supervisor reads to decide
// how to spawn and manage Claude subprocesses.
//
// Each profile is registered from its own file (blue.go, grey.go, ...)
// so any operator can read the full definition of a single variant
// without wading through the others. The operator-facing narrative
// lives in docs/variants/.
package variant

// (Methods live in profile.go; registry + built-in wiring live in
// registry.go so this file stays under 300 LOC and focuses on the
// canonical type system.)

// Name is the canonical variant identifier used internally in this
// package (e.g. "blue", "fixit"). This is the operator-facing CLI
// --variant value and the key used by voice.Variant.
//
// Docs and CLI use the longer "-ralph" display form in prose
// ("blue-ralph", "fixit-ralph"), but the code keeps the shorter form
// because every identifier in this package is already variant-scoped.
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

// BiasCategory identifies a helper-capability bias slot declared by the variant.
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
// a matching helper in the inventory.
//
// Placeholder: {skill} expands to the chosen helper's full name.
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
	// passed on every single `radioactive_ralph run` — never cached in config.
	FreshConfirmPerInvocation bool

	// RefuseServiceContext means the variant will not run when detected
	// under launchd/systemd (savage, old-man, world-breaker).
	RefuseServiceContext bool

	// RequireSpendCap means `radioactive_ralph run` fails without a cap value
	// either via --spend-cap-usd flag or [variants.X] spend_cap_usd.
	RequireSpendCap bool
}

// Profile is the canonical description of a variant.
type Profile struct {
	Name                 Name
	Description          string // one-liner shown in `radioactive_ralph --help` and `radioactive_ralph init`
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

	// ShellExplicitlyTrusted lets a shared-isolation variant include
	// Bash in its allowlist despite the structural "shared = read-only"
	// invariant. Bash is strictly more powerful than Edit+Write (it can
	// commit, rm, network-exec), so permitting it under shared
	// isolation is an explicit trust decision the profile declaration
	// must own. Blue sets it to true because its runtime posture restricts
	// Bash to read-focused queries; other
	// shared variants must follow the same discipline or refuse Bash.
	ShellExplicitlyTrusted bool
}
