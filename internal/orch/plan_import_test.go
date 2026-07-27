package orch

import (
	"context"
	"strings"
	"testing"

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
