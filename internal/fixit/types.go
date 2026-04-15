// Package fixit implements the deliberate plan-creation pipeline for
// fixit-ralph's advisor mode.
//
// See docs/design/fixit-plan-pipeline.md for the architectural
// rationale. Each stage in this package corresponds to a stage in the
// design doc.
package fixit

// IntentSpec captures Stage 1 output — what the operator is trying to
// accomplish and what's off-limits.
type IntentSpec struct {
	// Topic is the sanitized slug used in the output filename.
	Topic string

	// Description is free-form operator text describing the goal.
	// Either passed via --description, scraped from a TOPIC.md at the
	// repo root, or empty.
	Description string

	// Constraints are operator-declared hard limits. Examples: "no
	// opus", "stay under $5", "main branch only", "weekend only".
	// Stage 4 renders these as inviolable rules in the prompt; Stage
	// 5 rejects proposals that violate them.
	Constraints []string

	// AnswersToQs is the populated record of any interactive
	// questions the operator answered. Empty in non-interactive mode.
	AnswersToQs map[string]string
}

// GitCommit is a single entry from `git log --oneline` enriched with
// author + date.
type GitCommit struct {
	SHA     string
	Subject string
	Author  string
	DateISO string
}

// DocFile records a markdown file's frontmatter and basic metadata so
// Stage 4 can reason about doc freshness without re-reading every
// file.
type DocFile struct {
	Path        string
	Frontmatter map[string]string
	UpdatedISO  string // from frontmatter or file mtime fallback
}

// GHIssue is the subset of `gh pr/issue list --json` fields fixit
// uses.
type GHIssue struct {
	Number      int
	Title       string
	Draft       bool
	State       string // OPEN, CLOSED, MERGED
	MergeStatus string // CLEAN, DIRTY, BLOCKED, UNKNOWN
	Author      string
	Labels      []string
}

// InventorySnapshot is a flattened view of the capability inventory
// (skills, MCPs, agents). The full inventory.Inventory has more
// detail but the advisor only needs counts + names.
type InventorySnapshot struct {
	Skills []string // FullName form, e.g. "coderabbit:review"
	MCPs   []string
	Agents []string
}

// RepoContext is Stage 2 output — everything the deterministic
// exploration discovered about the repo.
type RepoContext struct {
	GitRoot         string
	CurrentBranch   string
	DefaultBranch   string
	OnDefaultBranch bool

	Commits []GitCommit

	DocsPresent []DocFile
	DocsStale   []string
	DocsMissing []string

	PlansDir         string
	PlansIndexExists bool
	PlansIndexFM     map[string]string
	PlansFiles       []string

	GHAuthenticated bool
	OpenPRs         []GHIssue
	OpenIssues      []GHIssue
	AIWelcomeIssues []GHIssue

	Inventory InventorySnapshot

	LangCounts        map[string]int
	GovernanceMissing []string
}

// VariantScore is one entry in Stage 3's deterministic ranking.
type VariantScore struct {
	Variant       string
	Score         int      // 0..100
	Reasons       []string // human-readable bullets, fed into prompt
	Disqualifying []string // hard exclusions; non-empty means score=0
}

// Task is one item in a PlanProposal's task list.
type Task struct {
	Title  string `json:"title"`
	Effort string `json:"effort"` // S | M | L
	Impact string `json:"impact"` // S | M | L
}

// PlanProposal is Stage 4 output — the structured JSON the constrained
// Claude subprocess returns.
type PlanProposal struct {
	Primary            string   `json:"primary"`
	PrimaryRationale   string   `json:"primary_rationale"`
	Alternate          string   `json:"alternate,omitempty"`
	AlternateWhen      string   `json:"alternate_when,omitempty"`
	Tasks              []Task   `json:"tasks"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	Confidence         int      `json:"confidence"` // 0..100
}

// ValidationResult is Stage 5 output — whether the proposal passed
// every rule and, if not, what failed.
type ValidationResult struct {
	Passed   bool
	Failures []string
}

// PlanStatus captures whether the emitted plan satisfies the plans-
// first discipline gate.
type PlanStatus string

const (
	// StatusCurrent means the plan passed every validation rule and
	// other variants will accept it as a valid plans/index.md target.
	StatusCurrent PlanStatus = "current"

	// StatusProvisional means at least one validation rule failed but
	// the proposal had enough merit to write. Other variants refuse to
	// run on a provisional plan until the operator promotes it.
	StatusProvisional PlanStatus = "provisional"

	// StatusFallback means Stage 4 (Claude analysis) failed twice. The
	// emitted file is a diagnostic, not a plan.
	StatusFallback PlanStatus = "fallback"
)

// EmittedPlan describes what Stage 6 wrote.
type EmittedPlan struct {
	Path       string
	Status     PlanStatus
	Proposal   PlanProposal // zero-valued when Status=fallback
	Validation ValidationResult
}
