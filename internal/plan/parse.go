// Package plan implements the heuristic markdown plan engine described in
// docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md §11.
//
// Plans are markdown documents parsed with goldmark into an AST and
// decomposed heuristically -- no LLM, no structured output, no vectors.
// The grammar:
//
//   - A heading of level N is a nesting group. Its "section" runs from the
//     heading to the next heading of level <= N.
//   - Heading order encodes group dependency: "# Do first" then "# Do next"
//     means the first group completes before the second (sequential groups
//     at the same level).
//   - Under a leaf heading (no child subheadings in its section): an
//     unordered list is parallelizable steps; an ordered list is sequential
//     steps. A step may carry paragraphs of detail (bullets + paragraphs
//     together form one step with detail).
//   - Do not descend past a heading that has child subheadings -- the
//     subheadings carry the ordering.
package plan

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

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
	// Text is the trimmed text of the list item itself.
	Text string

	// Detail is the trimmed, newline-joined text of any paragraphs found
	// in the same section as the list (narrative elaborating the step).
	// Empty when there is no such detail.
	Detail string
}

// Parse parses plan markdown into a Plan. Parse uses goldmark's core
// parser only (block + inline); GFM extensions (tables, strikethrough,
// autolinks, task-list checkboxes, etc.) are deliberately not enabled --
// the plan grammar is intentionally small.
func Parse(md []byte) (*Plan, error) {
	source := normalizeSource(md)
	gm := goldmark.DefaultParser()
	reader := text.NewReader(source)
	root := gm.Parse(reader)

	groups, err := parseSiblingHeadings(root.FirstChild(), 0, source)
	if err != nil {
		return nil, err
	}
	return &Plan{Groups: groups}, nil
}

// normalizeSource ensures the source ends with a newline, which keeps
// goldmark's line-segment bookkeeping well-formed for the last block in
// the document.
func normalizeSource(md []byte) []byte {
	if len(md) == 0 || md[len(md)-1] != '\n' {
		out := make([]byte, len(md)+1)
		copy(out, md)
		out[len(md)] = '\n'
		return out
	}
	return md
}

// parseSiblingHeadings scans the flat sibling chain starting at node,
// collecting every heading whose level is > parentLevel (0 at the
// document root) into Groups, each paired with the section that runs
// until the next heading of level <= that heading's own level.
func parseSiblingHeadings(node ast.Node, parentLevel int, source []byte) ([]Group, error) {
	var groups []Group

	for node != nil {
		heading, ok := node.(*ast.Heading)
		if !ok {
			// A non-heading block outside of any heading's section at
			// this scan depth (e.g. leading document narrative before
			// the first heading). It carries no step semantics on its
			// own, so it is skipped: plans are organized entirely by
			// heading, and narrative belongs inside a section.
			node = node.NextSibling()
			continue
		}

		if heading.Level <= parentLevel {
			// This heading closes the current scan; the caller's own
			// loop (one level up) will pick it up.
			break
		}

		sectionEnd := findSectionEnd(node, heading.Level)

		g, err := buildGroup(heading, node.NextSibling(), sectionEnd, source)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)

		node = sectionEnd
	}

	return groups, nil
}

// findSectionEnd returns the first sibling of headingNode, at or after
// headingNode, that is a heading of level <= level -- i.e. the node that
// starts the NEXT section at this level or shallower. Returns nil if the
// section runs to the end of the document.
func findSectionEnd(headingNode ast.Node, level int) ast.Node {
	for n := headingNode.NextSibling(); n != nil; n = n.NextSibling() {
		if h, ok := n.(*ast.Heading); ok && h.Level <= level {
			return n
		}
	}
	return nil
}

// buildGroup builds a single Group from its heading node and the sibling
// range [firstBodyNode, sectionEnd) that makes up its section body.
//
// A section is a leaf (Steps) unless it contains at least one child
// subheading (level > heading.Level), in which case it is a non-leaf
// (SubGroups) and buildGroup does not descend into list/paragraph content
// directly under it -- the subheadings carry the ordering, per the spec.
func buildGroup(heading *ast.Heading, firstBodyNode, sectionEnd ast.Node, source []byte) (Group, error) {
	g := Group{
		Heading: headingText(heading, source),
		Level:   heading.Level,
	}

	hasSubheading := false
	for n := firstBodyNode; n != sectionEnd && n != nil; n = n.NextSibling() {
		if _, ok := n.(*ast.Heading); ok {
			hasSubheading = true
			break
		}
	}

	if hasSubheading {
		subGroups, err := parseSiblingHeadings(firstBodyNode, heading.Level, source)
		if err != nil {
			return Group{}, err
		}
		g.SubGroups = subGroups
		return g, nil
	}

	steps, parallel := parseLeafSection(firstBodyNode, sectionEnd, source)
	g.Steps = steps
	g.Parallel = parallel
	return g, nil
}

// parseLeafSection scans the sibling range [node, sectionEnd) for a list
// (the section's steps) and any paragraphs (treated as shared detail
// appended to every step, per the spec's "bullets+paragraphs together =
// one step with detail" rule). If more than one list is present, the
// first one found determines Parallel and subsequent lists' items are
// appended to Steps in document order.
func parseLeafSection(node, sectionEnd ast.Node, source []byte) (steps []Step, parallel bool) {
	var details []string
	sawList := false

	for n := node; n != sectionEnd && n != nil; n = n.NextSibling() {
		switch v := n.(type) {
		case *ast.List:
			if !sawList {
				parallel = !v.IsOrdered()
				sawList = true
			}
			for item := v.FirstChild(); item != nil; item = item.NextSibling() {
				steps = append(steps, Step{Text: listItemText(item, source)})
			}
		case *ast.Paragraph:
			if t := strings.TrimSpace(string(v.Lines().Value(source))); t != "" {
				details = append(details, t)
			}
		}
	}

	if len(details) > 0 {
		detail := strings.Join(details, "\n\n")
		for i := range steps {
			steps[i].Detail = detail
		}
	}

	return steps, parallel
}

// headingText returns the trimmed source text of a heading line.
func headingText(h *ast.Heading, source []byte) string {
	return strings.TrimSpace(string(h.Lines().Value(source)))
}

// listItemText returns the trimmed, whitespace-collapsed text of a list
// item, concatenating every paragraph/text-block line inside it (a list
// item can itself wrap onto multiple source lines).
func listItemText(item ast.Node, source []byte) string {
	var parts []string
	for n := item.FirstChild(); n != nil; n = n.NextSibling() {
		if lines, ok := linesOf(n); ok {
			for i := range lines.Len() {
				seg := lines.At(i)
				if t := strings.TrimSpace(string(seg.Value(source))); t != "" {
					parts = append(parts, t)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

// linesOf returns the Lines() segments of a node, when it is a block type
// that carries them (Paragraph or TextBlock, the two shapes a list item's
// body takes under goldmark's core parser).
func linesOf(n ast.Node) (*text.Segments, bool) {
	switch v := n.(type) {
	case *ast.Paragraph:
		return v.Lines(), true
	case *ast.TextBlock:
		return v.Lines(), true
	default:
		return nil, false
	}
}
