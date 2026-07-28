package plan

import (
	"fmt"
	"strings"

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

// ValidationErrors is the import-blocking form of Validate's findings. Parse
// remains permissive for read-only inspection of historical plans, while
// ingress must fail closed instead of persisting a document whose dispatch
// order the heuristic grammar cannot determine.
type ValidationErrors []PlanError

func (errs ValidationErrors) Error() string {
	parts := make([]string, len(errs))
	for i, finding := range errs {
		parts[i] = finding.String()
	}
	return strings.Join(parts, "; ")
}

// ValidateForImport is the plan-ingress contract. It rejects empty/no-step
// documents and every ambiguity reported by Validate before a project or plan
// row is created. This is intentionally stricter than Parse, which remains
// useful for inspecting already-stored historical input.
func ValidateForImport(md []byte) error {
	if strings.TrimSpace(string(md)) == "" {
		return ValidationErrors{{Msg: "plan markdown is empty"}}
	}
	if findings := Validate(md); len(findings) > 0 {
		return ValidationErrors(findings)
	}
	parsed, err := Parse(md)
	if err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}
	if countPlanSteps(parsed.Groups) == 0 {
		return ValidationErrors{{Msg: "plan has no steps"}}
	}
	if findings := validateDifferentFrom(parsed); len(findings) > 0 {
		return ValidationErrors(findings)
	}
	return nil
}

// validateDifferentFrom checks every declared independence constraint resolves.
//
// An unresolvable reference is worse than an unenforced field: it is silently
// VACUOUS, so the plan looks as though it carries an independence guarantee
// while nothing can satisfy or violate it. A self-reference is the opposite
// failure — unsatisfiable, so dispatch could only ever block the task forever.
//
// Both are caught at import, where the author is present to fix them, rather
// than surfacing later as a review that was never actually independent or a task
// that mysteriously never runs.
func validateDifferentFrom(p *Plan) []PlanError {
	known := map[string]struct{}{}
	walkPlanSteps(p, func(id string, _ Step) {
		known[id] = struct{}{}
	})

	var findings []PlanError
	walkPlanSteps(p, func(id string, step Step) {
		if step.Metadata == nil {
			return
		}
		for _, peer := range step.Metadata.DifferentFrom {
			peer = strings.TrimSpace(peer)
			if peer == "" {
				continue
			}
			if peer == id {
				findings = append(findings, PlanError{Msg: fmt.Sprintf(
					"task %q declares differentFrom itself; a task cannot run on a "+
						"provider different from its own", id)})
				continue
			}
			if _, ok := known[peer]; !ok {
				findings = append(findings, PlanError{Msg: fmt.Sprintf(
					"task %q declares differentFrom %q, which is not a task in this "+
						"plan; the constraint could never be enforced", id, peer)})
			}
		}
	})
	return findings
}

// walkPlanSteps visits every step with the task id it will be imported under —
// the annotated id when present, the positional ref otherwise — so validation
// compares against the SAME ids dispatch will use.
func walkPlanSteps(p *Plan, fn func(id string, step Step)) {
	var walk func(groups []Group, path []int)
	walk = func(groups []Group, path []int) {
		for i, group := range groups {
			childPath := append(append([]int(nil), path...), i)
			for j, step := range group.Steps {
				ref := StepRef{GroupPath: childPath, Index: j}
				id := ref.ID()
				if step.Metadata != nil && step.Metadata.ID != "" {
					id = step.Metadata.ID
				}
				fn(id, step)
			}
			walk(group.SubGroups, childPath)
		}
	}
	walk(p.Groups, nil)
}

func countPlanSteps(groups []Group) int {
	total := 0
	for _, group := range groups {
		total += len(group.Steps)
		total += countPlanSteps(group.SubGroups)
	}
	return total
}

// Validate parses md and flags grammar ambiguities the heuristic decomposer
// (Parse/Decompose) has to guess through. Validate itself is advisory so
// callers can inspect historical input; ValidateForImport converts its
// findings into an ingress-blocking error. Findings are:
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
