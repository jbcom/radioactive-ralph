package orch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func importTestOrch(t *testing.T) (*Orchestrator, *store.Store, string) {
	t.Helper()
	st := newTestStore(t)
	projectID, err := st.CreateProject(context.Background(), "import-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return New(st), st, projectID
}

func readyIDs(t *testing.T, st *store.Store, planID string) []string {
	t.Helper()
	ready, err := st.Ready(context.Background(), planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	out := make([]string, 0, len(ready))
	for _, task := range ready {
		out = append(out, task.ID)
	}
	return out
}

// TestImportPlanSequentialPlanMaterializesChainEdges is the degenerate-case
// proof. A plan with no annotations must import as a chain, so dispatch walking
// task_deps resolves exactly the order plan.Decompose resolved positionally —
// that equivalence is what makes a linear plan a DAG rather than a second path.
func TestImportPlanSequentialPlanMaterializesChainEdges(t *testing.T) {
	o, st, projectID := importTestOrch(t)
	planID, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "seq", Title: "Seq",
		Markdown: "# Group\n\n1. first\n2. second\n3. third\n",
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	if got := readyIDs(t, st, planID); len(got) != 1 {
		t.Fatalf("ready = %v, want exactly one step — an ordered list is sequential", got)
	}
}

// TestImportPlanParallelLeafReleasesTogether covers the other half of document
// order: an unordered list is dispatchable together.
func TestImportPlanParallelLeafReleasesTogether(t *testing.T) {
	o, st, projectID := importTestOrch(t)
	planID, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "par", Title: "Par",
		Markdown: "# Group\n\n- alpha\n- beta\n- gamma\n",
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	if got := readyIDs(t, st, planID); len(got) != 3 {
		t.Fatalf("ready = %v, want all three — an unordered list is parallel", got)
	}
}

// TestImportPlanSecondGroupWaitsOnFirst pins group ordering: heading order is
// dependency order, so nothing in group two is ready while group one is open.
func TestImportPlanSecondGroupWaitsOnFirst(t *testing.T) {
	o, st, projectID := importTestOrch(t)
	planID, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "groups", Title: "Groups",
		Markdown: "# First\n\n- a\n- b\n\n# Second\n\n- c\n",
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}
	got := readyIDs(t, st, planID)
	if len(got) != 2 {
		t.Fatalf("ready = %v, want only the first group's two steps", got)
	}
	for _, id := range got {
		if strings.HasPrefix(id, "1.") {
			t.Fatalf("a second-group step %q was ready before the first group finished", id)
		}
	}
}

// TestImportPlanExplicitAfterOverridesDocumentOrder proves an annotated step
// takes the edges its author declared, not the positional ones.
func TestImportPlanExplicitAfterOverridesDocumentOrder(t *testing.T) {
	o, st, projectID := importTestOrch(t)
	md := "# Group\n\n" +
		"1. build it\n\n   ```ralph-task\n   {\"id\": \"build\"}\n   ```\n\n" +
		"2. ship it\n\n   ```ralph-task\n   {\"id\": \"ship\", \"after\": [\"build\"]}\n   ```\n\n" +
		"3. announce it\n\n   ```ralph-task\n   {\"id\": \"announce\", \"after\": []}\n   ```\n"

	planID, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "explicit", Title: "Explicit", Markdown: md,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}

	got := readyIDs(t, st, planID)
	// "build" is first in document order; "announce" opted out with after: [].
	// "ship" waits on build. So exactly those two are ready.
	if len(got) != 2 {
		t.Fatalf("ready = %v, want build and announce", got)
	}
	found := map[string]bool{}
	for _, id := range got {
		found[id] = true
	}
	if !found["build"] || !found["announce"] {
		t.Fatalf("ready = %v, want [build announce]", got)
	}
	if found["ship"] {
		t.Fatal("ship was ready despite declaring after: [build]")
	}
}

// TestImportPlanRejectsUnknownAfterTarget fails closed rather than importing a
// plan whose declared edge points at nothing.
func TestImportPlanRejectsUnknownAfterTarget(t *testing.T) {
	o, _, projectID := importTestOrch(t)
	md := "# Group\n\n1. step\n\n   ```ralph-task\n   {\"id\": \"a\", \"after\": [\"ghost\"]}\n   ```\n"
	_, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "ghost", Title: "Ghost", Markdown: md,
	})
	if err == nil {
		t.Fatal("imported a plan declaring a dependency on a nonexistent task")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error = %v, want it to name the missing task", err)
	}
}

// TestImportPlanRejectsDuplicateTaskIDs refuses an ambiguous graph rather than
// letting one id silently win.
func TestImportPlanRejectsDuplicateTaskIDs(t *testing.T) {
	o, _, projectID := importTestOrch(t)
	md := "# Group\n\n" +
		"1. one\n\n   ```ralph-task\n   {\"id\": \"same\"}\n   ```\n\n" +
		"2. two\n\n   ```ralph-task\n   {\"id\": \"same\"}\n   ```\n"
	if _, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "dupe", Title: "Dupe", Markdown: md,
	}); err == nil {
		t.Fatal("imported a plan with two tasks sharing an id")
	}
}

// TestImportPlanRejectsInvalidMarkdownBeforeWriting keeps ingress fail-closed:
// nothing is persisted when validation rejects the document.
func TestImportPlanRejectsInvalidMarkdownBeforeWriting(t *testing.T) {
	o, st, projectID := importTestOrch(t)
	if _, err := o.ImportPlan(context.Background(), ImportPlanOpts{
		ProjectID: projectID, Slug: "empty", Title: "Empty", Markdown: "",
	}); err == nil {
		t.Fatal("imported an empty plan")
	}
	var n int
	if err := st.DB().QueryRow(
		`SELECT COUNT(*) FROM plans WHERE project_id = ?`, projectID,
	).Scan(&n); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if n != 0 {
		t.Fatalf("a rejected import left %d plan row(s)", n)
	}
}

// acceptancePlan carries an `accept:` marker, which derives a MECHANICAL
// acceptance check — rerun the command and require it to pass.
const acceptancePlan = "# Acceptance\n\n" +
	"1. build the thing `accept: go build ./...`\n"

// TestImportDerivesAcceptanceCriteria is a verification-integrity regression.
// VerifyAndComplete reads only the STORED acceptance_json; an empty value
// selects judgment-only verification, so a graph import that dropped the
// derived criteria would let non-empty worker evidence complete a task without
// ever rerunning the command the plan demanded.
func TestImportDerivesAcceptanceCriteria(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "acceptance-import")
	o := New(s, WithBindingResolver(fakeBindingResolver("codex", false)))

	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "acc", Title: "Acc", Markdown: acceptancePlan,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}

	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].AcceptanceJSON == "" {
		t.Fatal("imported task has empty acceptance_json; VerifyAndComplete would " +
			"fall back to judgment-only and complete the task without rerunning " +
			"the command the plan declared")
	}
	if !strings.Contains(tasks[0].AcceptanceJSON, "go build") {
		t.Fatalf("acceptance_json = %q, want it to carry the declared command",
			tasks[0].AcceptanceJSON)
	}
}

// TestDispatchUsesTheImportedGraphID is the regression for a duplicate-node
// bug the graph ingress introduced. Import created the annotated task "build";
// dispatch derived its own POSITIONAL id "0.0" and materialized a SECOND task
// for the same step. The plan then held two nodes for one piece of work, and
// claiming "build" left it marked running with no worker launched, because a
// non-positional id had no StepRef to parse back into.
//
// One id rule, shared by import and dispatch, is what prevents that.
func TestDispatchUsesTheImportedGraphID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "graph-id-dispatch")
	runner := &bindingRecordingRunner{}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("codex", false)),
	)

	md := "# Build\n\n1. build the thing\n\n" +
		"   ```ralph-task\n   {\"id\": \"build\"}\n   ```\n"
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "b", Title: "B", Markdown: md,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 — an annotated task must actually run", dispatched)
	}

	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		ids := make([]string, 0, len(tasks))
		for _, task := range tasks {
			ids = append(ids, task.ID)
		}
		t.Fatalf("tasks = %v, want exactly [build] — dispatch materialized a "+
			"duplicate positional node for a step import had already created", ids)
	}
	if tasks[0].ID != "build" {
		t.Fatalf("task id = %q, want the explicit graph id", tasks[0].ID)
	}
}

// sharedOutputPlan declares two independent tasks writing the SAME exclusive
// path. Nothing in the dependency graph orders them — neither consumes the
// other's result — so both are ready at once, and the reservation is the only
// thing standing between them and a concurrent clobber.
const sharedOutputPlan = "# Shared output\n\n" +
	"- alpha writes the artifact\n\n" +
	"  ```ralph-task\n" +
	`  {"id": "alpha", "after": [], "outputs": [{"path": "build/artifact.txt", "mode": "exclusive"}]}` + "\n" +
	"  ```\n\n" +
	"- beta writes the artifact too\n\n" +
	"  ```ralph-task\n" +
	`  {"id": "beta", "after": [], "outputs": [{"path": "build/artifact.txt", "mode": "exclusive"}]}` + "\n" +
	"  ```\n"

// TestImportPersistsOutputReservations closes the loop from plan text to
// enforcement: a declared output in the markdown must reach
// task_output_reservations, and the claim path must then refuse the second
// task while the first runs.
func TestImportPersistsOutputReservations(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "shared-output")
	o := New(s, WithBindingResolver(fakeBindingResolver("codex", false)))

	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "shared", Title: "Shared", Markdown: sharedOutputPlan,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}

	// Both are ready: no edge orders them.
	parts, err := s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	total := 0
	for _, p := range parts {
		total += len(p.Tasks)
	}
	if total != 2 {
		t.Fatalf("ready tasks = %d, want 2 — both are dependency-free", total)
	}

	sessionA, workerA := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimTask(ctx, planID, "alpha", sessionA, workerA); err != nil {
		t.Fatalf("claim alpha: %v", err)
	}
	sessionB, workerB := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimTask(ctx, planID, "beta", sessionB, workerB); !errors.Is(err, store.ErrOutputReserved) {
		t.Fatalf("claim beta err = %v, want ErrOutputReserved — the declared output "+
			"in the plan markdown must reach the reservation table and be enforced", err)
	}
}

// TestFanoutWithConflictingOutputsStillMakesProgress is the livelock. A native
// fan-out group claims every eligible task under ONE worker; when two of them
// declare the same output, the second claim returns ErrOutputReserved. Treating
// that as fatal made dispatchFanoutGroup release every prior claim and return
// an error, so the next pass repeated the identical sequence and NEITHER task
// could ever run.
//
// A reservation conflict is temporary by construction — the holder is running
// and will finish. It must be a SKIP, exactly like losing a claim race, not a
// fault.
func TestFanoutWithConflictingOutputsStillMakesProgress(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-conflict")

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "first"}, {AssistantOutput: "second"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // NativeFanout
	)
	planID, err := o.ImportPlan(ctx, ImportPlanOpts{
		ProjectID: projectID, Slug: "conflict", Title: "Conflict", Markdown: sharedOutputPlan,
	})
	if err != nil {
		t.Fatalf("ImportPlan: %v", err)
	}

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext returned an error for a reservation conflict: %v — a "+
			"conflict is temporary (the holder is running and will finish), so it "+
			"must skip like a lost claim race, not fault", err)
	}
	o.Wait()
	if dispatched == 0 {
		t.Fatal("nothing dispatched: the fan-out group livelocked on its own " +
			"reservation conflict, so neither task can ever run")
	}
}
