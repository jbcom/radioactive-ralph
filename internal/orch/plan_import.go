package orch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// ImportPlanOpts is one plan-import request.
type ImportPlanOpts struct {
	ProjectID string
	Slug      string
	Title     string
	Markdown  string
}

// ImportPlan validates a markdown plan, materializes its tasks and dependency
// edges, and activates it — in one transaction.
//
// This is the single ingress. There is no separate "legacy" path: a plan with no
// dependency annotations materializes a chain of edges from document order, so a
// linear plan is the DEGENERATE CASE of a DAG rather than a second code path.
// Dispatch then walks task_deps for both, which is the whole point of the
// integration.
func (o *Orchestrator) ImportPlan(ctx context.Context, opts ImportPlanOpts) (string, error) {
	if opts.ProjectID == "" || opts.Slug == "" || opts.Title == "" {
		return "", fmt.Errorf("orch: ProjectID, Slug, and Title required")
	}
	// Fail closed at ingress rather than persisting a document whose dispatch
	// order cannot be determined.
	if err := plan.ValidateForImport([]byte(opts.Markdown)); err != nil {
		return "", fmt.Errorf("orch: validate plan: %w", err)
	}
	parsed, err := plan.Parse([]byte(opts.Markdown))
	if err != nil {
		return "", fmt.Errorf("orch: parse plan: %w", err)
	}

	specs, err := graphSpecs(parsed)
	if err != nil {
		return "", err
	}

	planID, err := o.store.CreatePlanGraph(ctx, store.CreatePlanGraphOpts{
		CreatePlanOpts: store.CreatePlanOpts{
			ProjectID:      opts.ProjectID,
			Slug:           opts.Slug,
			Title:          opts.Title,
			SourceMarkdown: opts.Markdown,
		},
		Tasks:    specs,
		Activate: true,
	})
	if err != nil {
		return "", fmt.Errorf("orch: import plan: %w", err)
	}
	return planID, nil
}

// graphSpecs turns a parsed plan into task specs with their dependency edges.
//
// Edge derivation follows the four cases the grammar defines (see
// plan.TaskMetadata.After):
//
//   - no metadata block, or a block with `after` omitted → document order
//   - `after: []` → an explicit root, no incoming edges
//   - `after: [ids]` → exactly those edges
//
// Document order reproduces exactly what plan.Decompose computed positionally:
// a sequential leaf chains each step to its predecessor, a parallel leaf
// releases together but waits on the previous group's terminals, and groups run
// in document order. Encoding it as edges is what lets dispatch stop re-parsing
// the markdown to recover the same information.
func graphSpecs(parsed *plan.Plan) ([]store.GraphTaskSpec, error) {
	type node struct {
		ref   plan.StepRef
		step  plan.Step
		group string
	}
	var nodes []node
	walk(parsed.Groups, nil, func(ref plan.StepRef, step plan.Step, groupPath string) {
		nodes = append(nodes, node{ref: ref, step: step, group: groupPath})
	})
	if len(nodes) == 0 {
		return nil, fmt.Errorf("orch: plan has no steps")
	}

	// Task ids: an explicit metadata id when present, else the positional
	// StepRef id the store has always used.
	ids := make([]string, len(nodes))
	byID := map[string]int{}
	for i, n := range nodes {
		id := stepTaskID(n.ref, n.step)
		if prev, dup := byID[id]; dup {
			return nil, fmt.Errorf(
				"orch: duplicate task id %q (steps %d and %d)", id, prev, i)
		}
		ids[i] = id
		byID[id] = i
	}

	// Document-order predecessors, computed once: for each node, the ids that
	// must complete before it when no explicit `after` overrides them.
	docOrder := documentOrderEdges(parsed, ids)

	specs := make([]store.GraphTaskSpec, 0, len(nodes))
	for i, n := range nodes {
		deps := docOrder[i]
		if explicit, stated := n.step.Metadata.DependsOn(); stated {
			deps = explicit
			for _, dep := range deps {
				if _, ok := byID[dep]; !ok {
					return nil, fmt.Errorf(
						"orch: task %q declares after %q, which is not a task in this plan",
						ids[i], dep)
				}
			}
		}
		metadataJSON := "{}"
		if n.step.Metadata != nil {
			encoded, err := json.Marshal(n.step.Metadata)
			if err != nil {
				return nil, fmt.Errorf("orch: encode task metadata for %q: %w", ids[i], err)
			}
			metadataJSON = string(encoded)
		}
		team := ""
		if n.step.Metadata != nil {
			team = n.step.Metadata.Team
		}
		// Acceptance is derived at IMPORT, not left to dispatch. VerifyAndComplete
		// reads only the stored acceptance_json, and an empty value selects
		// judgment-only verification — so a task imported without its criteria
		// could be completed by non-empty worker evidence alone, never rerunning
		// the command or checking the file the plan demanded.
		acceptance, err := defaultAcceptanceJSON(n.step)
		if err != nil {
			return nil, fmt.Errorf("orch: build acceptance for %q: %w", ids[i], err)
		}
		specs = append(specs, store.GraphTaskSpec{
			CreateTaskOpts: store.CreateTaskOpts{
				ID:               ids[i],
				Description:      n.step.Text,
				AcceptanceJSON:   acceptance,
				RequiresApproval: n.step.RequiresApproval,
			},
			DependsOn:    deps,
			GroupPath:    n.group,
			TeamPath:     team,
			MetadataJSON: metadataJSON,
		})
	}
	return specs, nil
}

// walk visits every leaf step in document order, reporting its positional ref
// and the dotted group path dispatch uses to partition a ready wave.
func walk(groups []plan.Group, path []int, fn func(plan.StepRef, plan.Step, string)) {
	for i, g := range groups {
		childPath := append(append([]int{}, path...), i)
		if len(g.SubGroups) > 0 {
			walk(g.SubGroups, childPath, fn)
			continue
		}
		groupPath := plan.StepRef{GroupPath: childPath}.GroupID()
		for j, s := range g.Steps {
			fn(plan.StepRef{GroupPath: childPath, Index: j}, s, groupPath)
		}
	}
}

// documentOrderEdges reproduces plan.Decompose's positional readiness as
// explicit edges, indexed to match the walk order of ids.
//
// The rules are Decompose's, restated: within a SEQUENTIAL leaf each step waits
// on its predecessor; within a PARALLEL leaf the steps are mutually independent
// but all wait on the previous group's terminals; and groups themselves run in
// document order. A plan with no annotations therefore imports as a chain (or a
// chain of fans), and dispatch resolves exactly the order it resolved before.
func documentOrderEdges(parsed *plan.Plan, ids []string) [][]string {
	edges := make([][]string, len(ids))
	// terminals of the previously-completed leaf group: the ids a following
	// group must wait on.
	var prevTerminals []string
	idx := 0

	var visit func(groups []plan.Group)
	visit = func(groups []plan.Group) {
		for _, g := range groups {
			if len(g.SubGroups) > 0 {
				visit(g.SubGroups)
				continue
			}
			if len(g.Steps) == 0 {
				continue
			}
			start := idx
			for range g.Steps {
				switch {
				case g.Parallel, idx == start:
					// A parallel group's steps are mutually independent, and a
					// sequential group's FIRST step has no in-group predecessor.
					// Both are gated only on the prior group's terminals.
					edges[idx] = append([]string{}, prevTerminals...)
				default:
					// Every later step of a sequential group chains to the one
					// before it.
					edges[idx] = []string{ids[idx-1]}
				}
				idx++
			}
			// A parallel group's terminals are all its steps; a sequential
			// group's terminal is only its last step.
			if g.Parallel {
				prevTerminals = append([]string{}, ids[start:idx]...)
			} else {
				prevTerminals = []string{ids[idx-1]}
			}
		}
	}
	visit(parsed.Groups)
	return edges
}
