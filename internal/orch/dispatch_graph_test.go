package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// diamondPlan is the shape positional decomposition cannot express and the
// graph can: after "prepare" completes, two INDEPENDENT leaf groups become
// ready at the same instant. plan.Decompose would only ever hand back one
// group's steps, because it walks groups in document order and stops at the
// first with unfinished work.
const diamondPlan = `# Diamond

## Prepare

1. prepare the tree

   ` + "```ralph-task" + `
   {"id": "prepare"}
   ` + "```" + `

## Left

1. left one

   ` + "```ralph-task" + `
   {"id": "left-one", "after": ["prepare"]}
   ` + "```" + `

2. left two

   ` + "```ralph-task" + `
   {"id": "left-two", "after": ["prepare"]}
   ` + "```" + `

## Right

1. right one

   ` + "```ralph-task" + `
   {"id": "right-one", "after": ["prepare"]}
   ` + "```" + `
`

func mustImportPlan(t *testing.T, o *Orchestrator, projectID, slug, markdown string) string {
	t.Helper()
	planID, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: slug, Title: slug, Markdown: markdown,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	return planID
}

// completeTask finishes taskID the way a worker would, so the next DispatchNext
// sees the graph advance.
func completeTask(t *testing.T, s *store.Store, planID, taskID string) {
	t.Helper()
	ctx := context.Background()
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimTask(ctx, planID, taskID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask %s: %v", taskID, err)
	}
	if _, err := s.MarkDone(ctx, planID, taskID, sessionID, "{}"); err != nil {
		t.Fatalf("MarkDone %s: %v", taskID, err)
	}
}

// TestDispatchNextReleasesIndependentGroupsTogether is the increment's headline
// behavior: once the shared predecessor is done, BOTH downstream leaf groups
// are ready and dispatch must be able to run them concurrently. Positional
// decomposition can only see one group at a time, so a plan that fans into two
// branches serializes them for no reason.
func TestDispatchNextReleasesIndependentGroupsTogether(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "diamond-project")

	runner := &bindingRecordingRunner{}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("codex", false)), // no native fan-out
	)
	planID := mustImportPlan(t, o, projectID, "diamond", diamondPlan)

	completeTask(t, s, planID, "prepare")

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 3 {
		t.Fatalf("dispatched = %d, want 3 — left-one, left-two and right-one are all ready once prepare is done", dispatched)
	}
}

// TestDispatchNextFanoutNeverSpansLeafGroups is the guard for the specific bug
// group_path exists to prevent. Three tasks are ready at once, but they belong
// to TWO groups. A native-fan-out provider may take a whole group in one turn —
// it must NOT take the union, because a fan-out turn runs under a single group
// heading and a single resolved binding.
func TestDispatchNextFanoutNeverSpansLeafGroups(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "diamond-fanout")

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "left done"},
		{AssistantOutput: "right done"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // NativeFanout=true
	)
	planID := mustImportPlan(t, o, projectID, "diamond-fan", diamondPlan)

	completeTask(t, s, planID, "prepare")

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	calls := runner.callReqs()
	if len(calls) == 0 {
		t.Fatal("no provider turns ran")
	}
	// The two-step Left group may be delegated as one turn; the one-step Right
	// group is its own turn. What must never happen is a single prompt owning
	// steps from both.
	for i, c := range calls {
		mentionsLeft := containsAll(c.UserPrompt, "left one") || containsAll(c.UserPrompt, "left two")
		mentionsRight := containsAll(c.UserPrompt, "right one")
		if mentionsLeft && mentionsRight {
			t.Fatalf("turn %d fanned out ACROSS leaf groups; prompt = %q", i, c.UserPrompt)
		}
	}
}

// TestDispatchNextLinearPlanIsUnchanged is the degeneracy proof. A plan with no
// annotations must dispatch exactly as it did when readiness was computed
// positionally — one step at a time, in author order. If the graph path changed
// this, every existing plan would change behavior on upgrade.
func TestDispatchNextLinearPlanIsUnchanged(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "linear-project")

	runner := &bindingRecordingRunner{}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("codex", false)),
	)
	planID := mustImportPlan(t, o, projectID, "linear", twoStepSequentialPlan)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 — a sequential plan still releases one step at a time", dispatched)
	}
}
