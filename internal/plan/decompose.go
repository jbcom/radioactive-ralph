package plan

import (
	"fmt"
	"strconv"
	"strings"
)

// StepRef identifies one Step's position in a Plan: the path of
// zero-based indices through Group/SubGroups from the plan root, followed
// by the zero-based index into that leaf Group's Steps.
type StepRef struct {
	GroupPath []int
	Index     int
}

// ID returns a stable, deterministic string key for this step, suitable
// for use in a done-set. It is derived purely from position in the plan
// tree (e.g. "0.1.2"), not from step text, so it stays stable across
// re-parses of the same document and is independent of wording edits that
// don't change structure.
func (r StepRef) ID() string {
	parts := make([]string, 0, len(r.GroupPath)+1)
	for _, p := range r.GroupPath {
		parts = append(parts, strconv.Itoa(p))
	}
	parts = append(parts, strconv.Itoa(r.Index))
	return strings.Join(parts, ".")
}

// StepIDs returns the stable ID (see StepRef.ID) for every step in the
// plan, in document order. This is the full universe of valid keys for a
// done-set map, and is useful for validating/seeding one.
func (p *Plan) StepIDs() []string {
	var ids []string
	walkGroups(p.Groups, nil, func(ref StepRef, _ Step) {
		ids = append(ids, ref.ID())
	})
	return ids
}

// walkGroups walks every step in every group (recursing into subgroups)
// in document order, invoking fn with each step's StepRef.
func walkGroups(groups []Group, path []int, fn func(StepRef, Step)) {
	for i, g := range groups {
		childPath := append(append([]int{}, path...), i)
		if len(g.SubGroups) > 0 {
			walkGroups(g.SubGroups, childPath, fn)
			continue
		}
		for j, s := range g.Steps {
			fn(StepRef{GroupPath: childPath, Index: j}, s)
		}
	}
}

// groupDone reports whether every step under g (recursing into
// subgroups) is marked done.
func groupDone(g Group, path []int, done map[string]bool) bool {
	if len(g.SubGroups) > 0 {
		for i, sub := range g.SubGroups {
			if !groupDone(sub, append(append([]int{}, path...), i), done) {
				return false
			}
		}
		return true
	}
	for j := range g.Steps {
		ref := StepRef{GroupPath: path, Index: j}
		if !done[ref.ID()] {
			return false
		}
	}
	return true
}

// Decompose computes the PRESENT: what is dispatchable right now, given
// the plan structure and a done-set keyed by StepRef.ID().
//
// It walks groups in document order (document order encodes dependency:
// an earlier group must complete before a later one starts). Within the
// first not-fully-done group:
//   - if it has subgroups, Decompose recurses into the first incomplete
//     subgroup (subheadings carry the ordering, so earlier subgroups gate
//     later ones exactly like top-level groups do);
//   - at a leaf, if the group is Parallel, every not-done step is
//     returned together (they are dispatchable concurrently); otherwise
//     (sequential) only the first not-done step is returned, since later
//     steps depend on it completing.
//
// Decompose returns (nil, false) when every step in the plan is done.
func Decompose(p *Plan, done map[string]bool) (readyNow []Step, parallel bool) {
	ready, _, par, ok := decomposeGroups(p.Groups, nil, done)
	if !ok {
		return nil, false
	}
	return ready, par
}

// decomposeGroups finds the first not-fully-done group in groups (in
// document order) and returns its ready steps. ok is false when every
// group is done.
func decomposeGroups(groups []Group, path []int, done map[string]bool) (readyNow []Step, refs []StepRef, parallel bool, ok bool) {
	for i, g := range groups {
		childPath := append(append([]int{}, path...), i)
		if groupDone(g, childPath, done) {
			continue
		}

		if len(g.SubGroups) > 0 {
			return decomposeGroups(g.SubGroups, childPath, done)
		}

		return leafReady(g, childPath, done)
	}
	return nil, nil, false, false
}

// leafReady computes the ready steps for a single leaf group: all
// not-done steps when Parallel, else just the first not-done step.
func leafReady(g Group, path []int, done map[string]bool) (readyNow []Step, refs []StepRef, parallel bool, ok bool) {
	for j, s := range g.Steps {
		ref := StepRef{GroupPath: path, Index: j}
		if done[ref.ID()] {
			continue
		}
		readyNow = append(readyNow, s)
		refs = append(refs, ref)
		if !g.Parallel {
			break
		}
	}
	if len(readyNow) == 0 {
		return nil, nil, false, false
	}
	return readyNow, refs, g.Parallel, true
}

// DecomposeRefs is like Decompose but also returns the StepRef for each
// ready step, in the same order, so a caller can mark individual steps
// done via StepRef.ID() without recomputing positions.
func DecomposeRefs(p *Plan, done map[string]bool) (readyNow []Step, refs []StepRef, parallel bool) {
	ready, refs, par, ok := decomposeGroups(p.Groups, nil, done)
	if !ok {
		return nil, nil, false
	}
	return ready, refs, par
}

// StepAt resolves a StepRef back to its Step and owning Group, primarily
// for callers that received a StepRef from DecomposeRefs and need to
// re-fetch the current Step/Group (e.g. after a re-parse).
func (p *Plan) StepAt(ref StepRef) (Step, Group, error) {
	groups := p.Groups
	var g Group
	for depth, idx := range ref.GroupPath {
		if idx < 0 || idx >= len(groups) {
			return Step{}, Group{}, fmt.Errorf("plan: group index %d out of range at depth %d", idx, depth)
		}
		g = groups[idx]
		groups = g.SubGroups
	}
	if ref.Index < 0 || ref.Index >= len(g.Steps) {
		return Step{}, Group{}, fmt.Errorf("plan: step index %d out of range", ref.Index)
	}
	return g.Steps[ref.Index], g, nil
}
