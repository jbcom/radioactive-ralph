package plan

import (
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// PlanError describes one advisory ambiguity found in a plan document.
// Line is 1-based, matching editor conventions; it is 0 when the finding
// applies to the document as a whole rather than one location.
//
// part of this package's specified public API (see the Phase 6a plan
// engine grammar); calling it plan.Error would collide with the "Error()
// string" convention for the error interface, which this type
// deliberately does not implement (findings are advisory, not errors).
//
//nolint:revive // "PlanError" stutters as plan.PlanError, but the name is
type PlanError struct {
	Line int
	Msg  string
}

func (e PlanError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// Validate parses md and flags grammar ambiguities the heuristic decomposer
// (Parse/Decompose) has to guess through. Validate is advisory: it never
// blocks Parse from running, but a plan with findings is a plan whose
// dispatch order may not be what its author intended. Findings are:
//
//   - a section with both a list and a leading bare paragraph, which is
//     ambiguous under the disambiguation rule (list => step-group, bare
//     paragraph with no list => narrative) when the paragraph precedes
//     the list and could be misread as an intended first step;
//   - a section that mixes an ordered and an unordered list -- Parse
//     picks the first list's orderedness for Group.Parallel and silently
//     folds the rest in, which is very likely not what the author meant;
//   - an empty group: a heading whose section (recursing into
//     subheadings) has no steps at all.
func Validate(md []byte) []PlanError {
	source := normalizeSource(md)
	gm := goldmark.DefaultParser()
	reader := text.NewReader(source)
	root := gm.Parse(reader)

	var errs []PlanError
	validateSiblingHeadings(root.FirstChild(), 0, source, &errs)
	return errs
}

func validateSiblingHeadings(node ast.Node, parentLevel int, source []byte, errs *[]PlanError) {
	for node != nil {
		heading, ok := node.(*ast.Heading)
		if !ok {
			node = node.NextSibling()
			continue
		}
		if heading.Level <= parentLevel {
			return
		}

		sectionEnd := findSectionEnd(node, heading.Level)
		firstBody := node.NextSibling()

		hasSubheading := false
		for n := firstBody; n != sectionEnd && n != nil; n = n.NextSibling() {
			if _, ok := n.(*ast.Heading); ok {
				hasSubheading = true
				break
			}
		}

		if hasSubheading {
			validateSiblingHeadings(firstBody, heading.Level, source, errs)
		} else {
			validateLeafSection(heading, firstBody, sectionEnd, source, errs)
		}

		node = sectionEnd
	}
}

func validateLeafSection(heading *ast.Heading, firstBody, sectionEnd ast.Node, source []byte, errs *[]PlanError) {
	line := lineOf(heading, source)
	htext := headingText(heading, source)

	var lists []*ast.List
	leadingParagraph := false
	sawList := false

	for n := firstBody; n != sectionEnd && n != nil; n = n.NextSibling() {
		switch v := n.(type) {
		case *ast.List:
			lists = append(lists, v)
			sawList = true
		case *ast.Paragraph:
			if !sawList && len(v.Lines().Value(source)) > 0 {
				leadingParagraph = true
			}
		}
	}

	if len(lists) == 0 {
		*errs = append(*errs, PlanError{
			Line: line,
			Msg:  fmt.Sprintf("group %q has no steps: a leaf heading needs a list (unordered = parallel, ordered = sequential); a bare paragraph is treated as narrative, not a step", htext),
		})
		return
	}

	if leadingParagraph {
		*errs = append(*errs, PlanError{
			Line: line,
			Msg:  fmt.Sprintf("group %q has a paragraph before its list: it is treated as narrative/notes, not a step -- move it after the list (as step detail) or reorder if it was meant as a first step", htext),
		})
	}

	if len(lists) > 1 {
		orderedCount, unorderedCount := 0, 0
		for _, l := range lists {
			if l.IsOrdered() {
				orderedCount++
			} else {
				unorderedCount++
			}
		}
		if orderedCount > 0 && unorderedCount > 0 {
			*errs = append(*errs, PlanError{
				Line: line,
				Msg:  fmt.Sprintf("group %q mixes ordered and unordered lists: only the first list's type determines Parallel; split into subheadings to make both orderings explicit", htext),
			})
		}
	}
}

// lineOf returns the 1-based source line number a node starts on, or 0
// if it cannot be determined. ATX headings never get Pos() set by
// goldmark's parser (only setext headings and paragraphs do), so this
// prefers Lines() -- available on Heading/Paragraph/TextBlock via
// BaseBlock -- and only falls back to Pos() for node kinds that don't
// carry Lines().
func lineOf(n ast.Node, source []byte) int {
	var start int
	if segs, ok := linesOf(n); ok && segs.Len() > 0 {
		start = segs.At(0).Start
	} else if h, ok := n.(*ast.Heading); ok && h.Lines().Len() > 0 {
		start = h.Lines().At(0).Start
	} else {
		start = n.Pos()
	}
	if start < 0 {
		return 0
	}
	line := 1
	for i := 0; i < start && i < len(source); i++ {
		if source[i] == '\n' {
			line++
		}
	}
	return line
}
