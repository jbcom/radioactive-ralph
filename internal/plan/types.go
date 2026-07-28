package plan

// This file holds the plan type surface. The types live here rather than beside
// the parser so the shape of a plan is readable without wading through goldmark
// traversal, and so the dependency-edge metadata sits next to the Step it
// annotates.

import "strings"

// Plan is the parsed, nested representation of a plan document.
type Plan struct {
	// Groups holds the top-level (heading level 1) groups in document
	// order. Document order is dependency order: Groups[0] completes
	// before Groups[1] starts, and so on.
	Groups []Group
}

// Group is a single heading's section. A Group either carries Steps (it is
// a leaf: no child subheadings appear in its section) or SubGroups (it has
// child subheadings, which carry the ordering) -- never both.
type Group struct {
	// Heading is the trimmed text of the heading line.
	Heading string

	// Level is the heading level (1-6).
	Level int

	// Parallel is true when this leaf group's steps come from an
	// unordered list (dispatchable together). It is false for an
	// ordered-list leaf (steps run one at a time) and is meaningless
	// (left false) for a non-leaf group.
	Parallel bool

	// Steps holds this leaf group's steps in document order. Empty for
	// a non-leaf group.
	Steps []Step

	// SubGroups holds this group's child subheadings in document order.
	// Empty for a leaf group.
	SubGroups []Group
}

// Step is a single unit of work: the list item text plus any trailing
// paragraph(s) of detail found alongside the list under the same heading.
type Step struct {
	// Text is the trimmed text of the list item itself, with any recognized
	// trailing marker (see RequiresApproval) stripped off.
	Text string

	// Detail is the trimmed, newline-joined text of any paragraphs found
	// in the same section as the list (narrative elaborating the step).
	// Empty when there is no such detail.
	Detail string

	// RequiresApproval is true when the step carries the `[approval]` marker
	// (case-insensitive, at the end of the list-item text). Such a step is
	// materialized as a task in status 'ready_pending_approval': it is held
	// out of dispatch until an operator approves it (GUI/IPC ApproveTask),
	// which transitions it to 'ready' so it becomes claimable. This is the
	// human-in-the-loop gate — the producer for the approval flow the
	// observe/drive surface already exposes.
	RequiresApproval bool

	// Metadata is the decoded ```ralph-task block annotating this step, or nil
	// when the step carries no such block. Nil is the common case: every plan
	// written before this grammar existed has no annotated steps, and an
	// unannotated step is a perfectly valid graph node.
	Metadata *TaskMetadata
}

// TaskMetadata is the decoded contents of a step's ```ralph-task fenced block.
// It lets a plan author state execution facts the prose cannot: explicit
// dependency edges, a provider binding, and the files the step reads or writes.
type TaskMetadata struct {
	// ID is the stable task identifier. Without it a task's identity would be
	// its document position, so inserting a step above would silently rename
	// everything below and orphan every edge pointing at it.
	ID string `json:"id"`

	// After holds the ids this task depends on. Its three states are DISTINCT
	// and the distinction is load-bearing:
	//
	//   nil         — the key was omitted. Edges come from document order, the
	//                 same as a step with no metadata block at all. Annotating a
	//                 step (adding team:, binding:, …) must NOT silently change
	//                 where it sits in the graph.
	//   &[]string{} — explicitly empty. No incoming edges: a root, ready
	//                 immediately. This is the only way to opt out of document
	//                 order.
	//   &[...]      — exactly these edges. Document order is not additionally
	//                 applied; the author has taken ownership of this ordering.
	//
	// That is why this is *[]string and not []string: a plain slice cannot tell
	// "omitted" from "explicitly none", and collapsing them would make the same
	// plan import as two different graphs.
	After *[]string `json:"after"`

	// Team is a slash-delimited path used to group tasks in the operator views
	// and to keep independent teams' work from being folded into one fan-out.
	Team string `json:"team"`

	// Binding pins how this task is executed.
	Binding TaskBinding `json:"binding"`

	// Requires names capability keys the bound provider must satisfy. A task
	// whose requirements are unmet fails closed (blocked_capability) rather than
	// running against a provider that cannot do the work.
	Requires []string `json:"requires"`

	// Providers restricts this task to a subset of configured providers.
	Providers []string `json:"providers"`

	// DifferentFrom names tasks that must not share this task's independence
	// domain — for work whose value depends on being done by a different model.
	DifferentFrom []string `json:"differentFrom"`

	// Inputs are files this task reads, optionally pinned by hash.
	Inputs []TaskInput `json:"inputs"`

	// Outputs are files this task writes. An exclusive output cannot overlap a
	// concurrently running task's declared paths.
	Outputs []TaskOutput `json:"outputs"`
}

// TaskBinding pins the provider identity for one task.
type TaskBinding struct {
	Mode        string `json:"mode"`
	Alias       string `json:"alias"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	Calibration string `json:"calibration"`
	Repetitions int    `json:"repetitions"`
	Fixture     string `json:"fixture"`
}

// TaskInput is a file this task reads, optionally pinned to an exact content
// hash so a changed input is detected rather than silently used.
type TaskInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// TaskOutput is a file this task writes. Mode is currently always "exclusive".
type TaskOutput struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// DependsOn reports the explicit dependency ids and whether the author stated
// them at all. Callers deriving edges must branch on stated: when it is false,
// document order supplies the edges.
func (m *TaskMetadata) DependsOn() (ids []string, stated bool) {
	if m == nil || m.After == nil {
		return nil, false
	}
	return *m.After, true
}

// AllowedProviders returns the task's provider restriction, tolerating a nil
// receiver so dispatch can ask without a nil check at every call site.
func (m *TaskMetadata) AllowedProviders() []string {
	if m == nil {
		return nil
	}
	return m.Providers
}

// PinnedProviderType returns the provider TYPE a task's binding pins, or "".
//
// Deliberately separate from AllowedProviders, which was the first attempt and
// was wrong. CheckAllowedProviders matches an entry against either the binding
// ALIAS or its type, which is right for `providers` -- an operator naming
// "reviewer" means that configured binding. It is wrong for a pin: an alias
// merely NAMED "codex" that is backed by type "claude" would satisfy
// binding.provider="codex" and run the task on Claude, honouring the
// declaration's spelling rather than its meaning.
//
// So the pin is checked against Config.Type ALONE, at dispatch, where the
// resolved binding is known. Import cannot decide this: alias-to-type mapping
// lives in operator config that ValidateForImport never sees.
func (m *TaskMetadata) PinnedProviderType() string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Binding.Provider)
}

// IndependencePeers returns the tasks this one must not share an independence
// domain with, tolerating a nil receiver for the same reason AllowedProviders
// does.
func (m *TaskMetadata) IndependencePeers() []string {
	if m == nil {
		return nil
	}
	return m.DifferentFrom
}
