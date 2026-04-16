// Package voice renders Ralph's personality for each variant.
//
// Every user-facing emission from the repo service — pre-flight questions,
// startup banners, status lines, attach-stream event renderings,
// shutdown messages, spend-cap notifications — gets voiced through
// this package. The same fact emits different copy per variant so the
// operator experiences the distinct personalities documented in
// docs/variants/.
//
// Today the package ships a complete fallback library plus custom
// templates for green (the classic) and blue (the observer). Variants
// without bespoke voice copy currently fall back to the generic event
// templates.
//
// Design: templates are plain Go string/template-literal pairs rather
// than text/template. We deliberately don't use text/template — the
// interpolation surface is narrow (pr number, repo slug, count, reason,
// branch, usd) and the control-flow features of text/template are a
// foot-gun when operators can tweak voice via config.toml later.
package voice

import (
	"fmt"
	"strings"
	"sync"
)

// Variant is an identifier matching the variant profile. It stays in
// this package so voice rendering can remain decoupled from the
// variant package's runtime profile types.
type Variant string

// Canonical variant names. Kept in sync with the variant registry and
// the operator-facing docs/variants pages.
const (
	VariantGreen        Variant = "green"
	VariantGrey         Variant = "grey"
	VariantRed          Variant = "red"
	VariantBlue         Variant = "blue"
	VariantProfessor    Variant = "professor"
	VariantFixit        Variant = "fixit"
	VariantImmortal     Variant = "immortal"
	VariantSavage       Variant = "savage"
	VariantOldMan       Variant = "old-man"
	VariantWorldBreaker Variant = "world-breaker"
)

// Event is the kind-of-emission key. Small, documented set.
type Event string

// Canonical events the runtime voices. Adding new events requires
// adding templates (or accepting fallbacks) for every variant you care
// about.
const (
	EventStartup        Event = "startup"
	EventShutdown       Event = "shutdown"
	EventCycleStart     Event = "cycle.start"
	EventCycleEnd       Event = "cycle.end"
	EventSessionSpawn   Event = "session.spawn"
	EventSessionDeath   Event = "session.death"
	EventSessionResume  Event = "session.resume"
	EventTaskClaim      Event = "task.claim"
	EventTaskDone       Event = "task.done"
	EventPRMerge        Event = "pr.merge"
	EventPRMergeFailed  Event = "pr.merge.failed"
	EventReviewApproved Event = "review.approved"
	EventReviewChanges  Event = "review.changes"
	EventSpendCapHit    Event = "spend.cap"
	EventGateRefusal    Event = "gate.refusal"
)

// Fields are the values substituted into templates. Only the subset
// relevant to the event is consulted — renderers gracefully tolerate
// zero values.
type Fields struct {
	Repo     string
	Branch   string
	PRNumber int
	TaskID   string
	Count    int    // severity count, cycle number, etc.
	Reason   string // exit reason, refusal reason, etc.
	USD      string // pre-formatted spend string (e.g. "$42.50")
	Extra    string // catch-all when one more string is needed
}

// registry holds the per-variant template library plus a default fallback.
type registry struct {
	mu        sync.RWMutex
	templates map[Variant]map[Event]string
	fallback  map[Event]string
}

var defaultRegistry = &registry{
	templates: make(map[Variant]map[Event]string),
	fallback:  make(map[Event]string),
}

func init() {
	registerGreen()
	registerBlue()
	registerFallback()
}

// Say returns Ralph's voiced message for a given variant + event.
// Falls back to the default template if the variant hasn't registered
// one; falls back to a canonical event name if no template exists at
// all. Never panics.
func Say(variant Variant, event Event, f Fields) string {
	defaultRegistry.mu.RLock()
	tpls := defaultRegistry.templates[variant]
	tpl, ok := tpls[event]
	if !ok {
		tpl = defaultRegistry.fallback[event]
	}
	defaultRegistry.mu.RUnlock()
	if tpl == "" {
		return fmt.Sprintf("[%s/%s]", variant, event)
	}
	return interpolate(tpl, f)
}

// Register adds or replaces a template for one variant + event.
// Called by the variant package at init time; tests can also use it
// to override specific templates and inspect the output.
func Register(variant Variant, event Event, template string) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	if defaultRegistry.templates[variant] == nil {
		defaultRegistry.templates[variant] = make(map[Event]string)
	}
	defaultRegistry.templates[variant][event] = template
}

// RegisterFallback sets the default template used when a variant
// lacks a specific event template.
func RegisterFallback(event Event, template string) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.fallback[event] = template
}

// ResetForTesting clears the registry and re-registers the built-in
// variants. Tests that mutate the registry call this in a t.Cleanup
// to avoid bleeding state between tests.
func ResetForTesting() {
	defaultRegistry.mu.Lock()
	defaultRegistry.templates = make(map[Variant]map[Event]string)
	defaultRegistry.fallback = make(map[Event]string)
	defaultRegistry.mu.Unlock()
	// Re-register outside the lock — Register takes its own write lock,
	// and sync.RWMutex is not re-entrant.
	registerGreen()
	registerBlue()
	registerFallback()
}

// interpolate replaces {repo} {branch} {pr} {task} {count} {reason}
// {usd} {extra} placeholders with the corresponding Fields value.
// Missing values become empty strings (so "merged PR #{pr}" with
// PRNumber=0 produces "merged PR #0", callers are responsible for not
// rendering events they have no fields for).
func interpolate(tpl string, f Fields) string {
	r := strings.NewReplacer(
		"{repo}", f.Repo,
		"{branch}", f.Branch,
		"{pr}", fmt.Sprintf("%d", f.PRNumber),
		"{task}", f.TaskID,
		"{count}", fmt.Sprintf("%d", f.Count),
		"{reason}", f.Reason,
		"{usd}", f.USD,
		"{extra}", f.Extra,
	)
	return r.Replace(tpl)
}

// ---- built-in templates -----------------------------------------------

func registerGreen() {
	Register(VariantGreen, EventStartup, "Ralph's at work. Let's see what needs doing.")
	Register(VariantGreen, EventShutdown, "Closing up shop.")
	Register(VariantGreen, EventCycleStart, "New cycle. Scanning the portfolio.")
	Register(VariantGreen, EventSessionSpawn, "Spawning a session on {repo} ({branch}).")
	Register(VariantGreen, EventSessionDeath, "Session on {repo} ended: {reason}.")
	Register(VariantGreen, EventSessionResume, "Resuming session on {repo}.")
	Register(VariantGreen, EventTaskClaim, "Claiming task {task}.")
	Register(VariantGreen, EventTaskDone, "Task {task} done.")
	Register(VariantGreen, EventPRMerge, "Merged PR #{pr} on {repo}.")
	Register(VariantGreen, EventPRMergeFailed, "Couldn't merge PR #{pr} on {repo}: {reason}.")
	Register(VariantGreen, EventReviewApproved, "Review approved for PR #{pr}.")
	Register(VariantGreen, EventReviewChanges, "Review found {count} issues on PR #{pr}.")
	Register(VariantGreen, EventSpendCapHit, "Spend cap hit ({usd}). Ralph's going home.")
}

func registerBlue() {
	Register(VariantBlue, EventStartup, "Blue Ralph here, observing only. No writes.")
	Register(VariantBlue, EventShutdown, "Observation complete.")
	Register(VariantBlue, EventCycleStart, "Scanning PRs on {repo}.")
	Register(VariantBlue, EventReviewApproved, "PR #{pr} looks clean.")
	Register(VariantBlue, EventReviewChanges, "PR #{pr}: {count} things to flag.")
}

func registerFallback() {
	RegisterFallback(EventStartup, "Ralph starting.")
	RegisterFallback(EventShutdown, "Ralph stopping.")
	RegisterFallback(EventCycleStart, "Starting cycle.")
	RegisterFallback(EventCycleEnd, "Cycle complete.")
	RegisterFallback(EventSessionSpawn, "Session spawned on {repo}.")
	RegisterFallback(EventSessionDeath, "Session ended: {reason}.")
	RegisterFallback(EventSessionResume, "Resuming session.")
	RegisterFallback(EventTaskClaim, "Claiming task {task}.")
	RegisterFallback(EventTaskDone, "Task {task} done.")
	RegisterFallback(EventPRMerge, "PR #{pr} merged.")
	RegisterFallback(EventPRMergeFailed, "PR #{pr} merge failed: {reason}.")
	RegisterFallback(EventReviewApproved, "PR #{pr} approved.")
	RegisterFallback(EventReviewChanges, "PR #{pr}: {count} issues.")
	RegisterFallback(EventSpendCapHit, "Spend cap hit ({usd}).")
	RegisterFallback(EventGateRefusal, "Refusing to run: {reason}.")
}
