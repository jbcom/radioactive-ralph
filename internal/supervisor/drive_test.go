package supervisor

import (
	"context"
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

func TestHandlePlanImport_DuplicateSlugIsConflict(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, _ := sup.store.CreateProject(ctx, "p", []store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	args := ipc.PlanImportArgs{Markdown: "# Same\n", Project: projectID}
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
	reply, _ := sup.HandlePlanImport(ctx, ipc.PlanImportArgs{Markdown: "# P\n", Project: projectID})

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
