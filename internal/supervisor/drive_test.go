package supervisor

import (
	"context"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestHandlePlanImport_CreatesActivePlan(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	reply, err := sup.HandlePlanImport(ctx, ipc.PlanImportArgs{
		Markdown: "# Ship It\n\n1. do the thing\n", Project: projectID,
	})
	if err != nil {
		t.Fatalf("HandlePlanImport: %v", err)
	}
	if reply.Title != "Ship It" || reply.Slug != "ship-it" {
		t.Errorf("reply = %+v, want Ship It/ship-it", reply)
	}
	plans, _ := sup.store.ListPlans(ctx, projectID, nil) // active+paused
	if len(plans) != 1 || plans[0].Status != store.PlanStatusActive {
		t.Errorf("plan not created active: %+v", plans)
	}
}

func TestHandlePlanImport_MissingProjectIsInvalidArgs(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	_, err := sup.HandlePlanImport(context.Background(), ipc.PlanImportArgs{Markdown: "# x"})
	if !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("err = %v, want CodeInvalidArgs", err)
	}
}

func TestHandlePlanImport_InvalidGrammarIsInvalidArgsAndCreatesNothing(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: t.TempDir(),
	}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	_, err = sup.HandlePlanImport(ctx, ipc.PlanImportArgs{
		Project:  projectID,
		Markdown: "# Ambiguous\n\nThis paragraph comes first.\n\n- actual step\n",
	})
	if !ipc.IsCode(err, ipc.CodeInvalidArgs) || !strings.Contains(err.Error(), "paragraph before its list") {
		t.Fatalf("invalid plan error = %v, want CodeInvalidArgs grammar finding", err)
	}
	plans, listErr := sup.store.ListPlans(ctx, projectID, []store.PlanStatus{
		store.PlanStatusDraft,
		store.PlanStatusActive,
	})
	if listErr != nil {
		t.Fatalf("ListPlans: %v", listErr)
	}
	if len(plans) != 0 {
		t.Fatalf("invalid supervisor import created plans: %+v", plans)
	}
}

func TestHandlePlanImport_DuplicateSlugIsConflict(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, _ := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	args := ipc.PlanImportArgs{Markdown: "# Same\n\n1. do the work\n", Project: projectID}
	if _, err := sup.HandlePlanImport(ctx, args); err != nil {
		t.Fatalf("first import: %v", err)
	}
	_, err := sup.HandlePlanImport(ctx, args)
	if !ipc.IsCode(err, ipc.CodeConflict) {
		t.Errorf("duplicate err = %v, want CodeConflict", err)
	}
}

func TestHandlePlanSetStatus_ValidatesTransition(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, _ := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	reply, _ := sup.HandlePlanImport(ctx, ipc.PlanImportArgs{
		Markdown: "# P\n\n1. do the work\n", Project: projectID,
	})

	// A valid pause.
	if _, err := sup.HandlePlanSetStatus(ctx, ipc.PlanSetStatusArgs{PlanID: reply.PlanID, Status: "paused"}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// An illegal status.
	if _, err := sup.HandlePlanSetStatus(ctx, ipc.PlanSetStatusArgs{PlanID: reply.PlanID, Status: "bananas"}); !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("illegal status err = %v, want CodeInvalidArgs", err)
	}
	// Unknown plan.
	if _, err := sup.HandlePlanSetStatus(ctx, ipc.PlanSetStatusArgs{PlanID: "nope", Status: "active"}); !ipc.IsCode(err, ipc.CodeNotFound) {
		t.Errorf("unknown plan err = %v, want CodeNotFound", err)
	}
}

func TestHandleWorkerKill_UnknownIsNotFound(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	err := sup.HandleWorkerKill(context.Background(), ipc.WorkerKillArgs{WorkerID: "ghost"})
	if !ipc.IsCode(err, ipc.CodeNotFound) {
		t.Errorf("err = %v, want CodeNotFound", err)
	}
}

func TestHandleTaskApprove_UnknownIsNotFound(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	err := sup.HandleTaskApprove(context.Background(), ipc.TaskApproveArgs{PlanID: "p", TaskID: "t"})
	if !ipc.IsCode(err, ipc.CodeNotFound) {
		t.Errorf("err = %v, want CodeNotFound", err)
	}
}

// TestDriveHandlers_ValidateEmptyArgs proves the drive handlers reject empty
// required ids with CodeInvalidArgs (a clean, specific error) rather than
// letting a blank id reach the store and surface as a confusing query miss —
// consistent across HandlePlanSetStatus, HandleTaskApprove, HandleWorkerKill.
func TestDriveHandlers_ValidateEmptyArgs(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()

	if _, err := sup.HandlePlanSetStatus(ctx, ipc.PlanSetStatusArgs{PlanID: "", Status: "active"}); !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("plan-set-status empty plan_id err = %v, want CodeInvalidArgs", err)
	}
	if err := sup.HandleTaskApprove(ctx, ipc.TaskApproveArgs{PlanID: "", TaskID: "t"}); !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("task-approve empty plan_id err = %v, want CodeInvalidArgs", err)
	}
	if err := sup.HandleTaskApprove(ctx, ipc.TaskApproveArgs{PlanID: "p", TaskID: ""}); !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("task-approve empty task_id err = %v, want CodeInvalidArgs", err)
	}
	if err := sup.HandleWorkerKill(ctx, ipc.WorkerKillArgs{WorkerID: ""}); !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("worker-kill empty worker_id err = %v, want CodeInvalidArgs", err)
	}
}

// TestHandlePlanImportMaterializesTheGraph is the P1 that mattered most: the
// graph ingress existed but nothing user-facing reached it. The supervisor's
// plan-import handler called CreatePlan + SetPlanStatus directly, so a plan
// whose steps declare `after:` edges imported with NO edges and NO metadata —
// user-authored ordering was silently discarded and the plan ran in document
// order instead.
func TestHandlePlanImportMaterializesTheGraph(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "graphproj",
		[]store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	markdown := "# Graph\n\n" +
		"1. prepare\n\n" +
		"   ```ralph-task\n   {\"id\": \"prepare\"}\n   ```\n\n" +
		"2. depends on prepare\n\n" +
		"   ```ralph-task\n   {\"id\": \"after-prepare\", \"after\": [\"prepare\"]}\n   ```\n"

	reply, importErr := sup.HandlePlanImport(ctx, ipc.PlanImportArgs{
		Project: projectID, Markdown: markdown, Title: "Graph",
	})
	if importErr != nil {
		t.Fatalf("HandlePlanImport: %v", importErr)
	}

	tasks, err := sup.store.ListTasks(ctx, reply.PlanID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 — the import must materialize the graph, "+
			"not just store the markdown", len(tasks))
	}
	byID := map[string]bool{}
	for _, task := range tasks {
		byID[task.ID] = true
	}
	if !byID["prepare"] || !byID["after-prepare"] {
		t.Fatalf("task ids = %v, want the EXPLICIT graph ids from the plan", byID)
	}

	// The edge must be real: only the root is ready.
	ready, err := sup.store.Ready(ctx, reply.PlanID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "prepare" {
		t.Fatalf("ready = %v, want exactly [prepare] — the declared edge was dropped",
			ready)
	}
}

// TestHandlePlanDelete_RemovesThePlanAndItsTasks covers the surface that makes
// store.DeletePlan reachable, and the guard a reviewer caught missing.
//
// The delete must also cancel any live worker on that plan. Removing task rows
// does NOT stop the agent subprocess: without the kill, a `plan delete` on an
// active plan leaves the provider running -- spending tokens and mutating the
// checkout -- until its turn deadline, with its post-run writes then failing
// against rows that no longer exist.
//
// KillWorker is best-effort and returns false when no live run is registered,
// so this asserts the store-visible outcome: the plan and its tasks are gone,
// and a worker that claimed one of them no longer holds it.
func TestHandlePlanDelete_RemovesThePlanAndItsTasks(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, _ := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	reply, err := sup.HandlePlanImport(ctx, ipc.PlanImportArgs{
		Markdown: "# P\n\n1. do the work\n", Project: projectID,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if _, err := sup.HandlePlanDelete(ctx, ipc.PlanDeleteArgs{PlanID: reply.PlanID}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The plan is gone: a second delete finds nothing.
	if _, err := sup.HandlePlanDelete(ctx, ipc.PlanDeleteArgs{PlanID: reply.PlanID}); err == nil {
		t.Error("deleting an already-deleted plan succeeded; the first delete " +
			"did not remove it, or the handler cannot tell missing from present")
	}

	// And its tasks went with it -- the point of the retention primitive.
	var n int
	if err := sup.store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE plan_id = ?", reply.PlanID).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if n != 0 {
		t.Errorf("%d task rows survived the plan delete; the page this exists to "+
			"unsaturate would stay full", n)
	}

	// An empty id must be rejected rather than deleting something arbitrary.
	if _, err := sup.HandlePlanDelete(ctx, ipc.PlanDeleteArgs{}); !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Errorf("empty plan_id err = %v, want CodeInvalidArgs", err)
	}
}

// TestHandlePlanDelete_PreservesPlanWhenWorkerDiscoveryFails proves deletion
// fails closed. If active-worker discovery is unavailable, deleting the plan
// could leave an undiscovered provider mutating the checkout with its durable
// task rows already gone.
func TestHandlePlanDelete_PreservesPlanWhenWorkerDiscoveryFails(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{
		Kind: store.FingerprintKindAbsPath, Value: t.TempDir(),
	}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	reply, err := sup.HandlePlanImport(ctx, ipc.PlanImportArgs{
		Markdown: "# P\n\n1. do the work\n", Project: projectID,
	})
	if err != nil {
		t.Fatalf("HandlePlanImport: %v", err)
	}

	// Rename only the discovery table. SQLite rewrites foreign-key references
	// to the new name, so this makes ListRunningWorkers fail while leaving the
	// plan table independently queryable for the postcondition.
	if _, err := sup.store.DB().ExecContext(ctx, "ALTER TABLE workers RENAME TO unavailable_workers"); err != nil {
		t.Fatalf("break worker discovery fixture: %v", err)
	}

	if _, err := sup.HandlePlanDelete(ctx, ipc.PlanDeleteArgs{PlanID: reply.PlanID}); err == nil ||
		!strings.Contains(err.Error(), "list running workers") {
		t.Fatalf("HandlePlanDelete discovery error = %v, want list-running-workers failure", err)
	}

	var plans int
	if err := sup.store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plans WHERE id = ?", reply.PlanID).Scan(&plans); err != nil {
		t.Fatalf("count preserved plan: %v", err)
	}
	if plans != 1 {
		t.Fatalf("plan rows after worker discovery failure = %d, want 1; deletion must fail closed", plans)
	}
}
