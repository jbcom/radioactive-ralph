package supervisor

import (
	"context"
	"errors"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// isDuplicateSlug reports whether err is the store's duplicate-slug sentinel.
func isDuplicateSlug(err error) bool { return errors.Is(err, store.ErrDuplicateSlug) }

// isPlanNotFound reports whether err is the store's plan-not-found sentinel.
func isPlanNotFound(err error) bool { return errors.Is(err, store.ErrPlanNotFound) }

// The Supervisor implements ipc.DriveHandler (the v2 drive surface) in
// addition to the v1 observe Handler. These mutations funnel through the
// supervisor so there is one writer of record for drive actions, and each
// reuses the same store/orchestrator the supervisor already owns.

// codedError attaches an ipc error-class code to an error so the server maps
// it onto Response.Code. It satisfies the ipc.Coded interface the server
// checks when writing a failure response.
type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string { return e.msg }
func (e *codedError) Code() string  { return e.code }

// HandlePlanImport creates a plan from markdown and activates it — the same
// logic the `plan import` CLI runs, moved server-side.
func (s *Supervisor) HandlePlanImport(ctx context.Context, args ipc.PlanImportArgs) (ipc.PlanImportReply, error) {
	var zero ipc.PlanImportReply
	if args.Project == "" {
		return zero, &codedError{ipc.CodeInvalidArgs, "plan-import: project id required"}
	}
	if len(args.Markdown) == 0 {
		return zero, &codedError{ipc.CodeInvalidArgs, "plan-import: markdown required"}
	}

	title := args.Title
	if title == "" {
		title = plan.Title(args.Markdown, "plan")
	}
	slug := args.Slug
	if slug == "" {
		slug = plan.Slug(title)
	}

	planID, err := s.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      args.Project,
		Slug:           slug,
		Title:          title,
		SourceMarkdown: args.Markdown,
	})
	if err != nil {
		if isDuplicateSlug(err) {
			return zero, &codedError{ipc.CodeConflict, err.Error()}
		}
		return zero, fmt.Errorf("supervisor: create plan: %w", err)
	}
	if err := s.store.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		return zero, fmt.Errorf("supervisor: activate plan: %w", err)
	}
	return ipc.PlanImportReply{PlanID: planID, Slug: slug, Title: title}, nil
}

// allowedPlanStatuses are the transitions the drive API permits an operator to
// request (pause/resume/abandon). Other store statuses are internal.
var allowedPlanStatuses = map[string]store.PlanStatus{
	"paused":    store.PlanStatusPaused,
	"active":    store.PlanStatusActive,
	"abandoned": store.PlanStatusAbandoned,
}

// HandlePlanSetStatus changes a plan's lifecycle status, validated to the
// allowed operator transitions.
func (s *Supervisor) HandlePlanSetStatus(ctx context.Context, args ipc.PlanSetStatusArgs) (ipc.PlanSetStatusReply, error) {
	var zero ipc.PlanSetStatusReply
	if args.PlanID == "" {
		return zero, &codedError{ipc.CodeInvalidArgs, "plan-set-status: plan_id required"}
	}
	target, ok := allowedPlanStatuses[args.Status]
	if !ok {
		return zero, &codedError{ipc.CodeInvalidArgs, fmt.Sprintf("plan-set-status: %q is not an allowed status (paused|active|abandoned)", args.Status)}
	}
	if err := s.store.SetPlanStatus(ctx, args.PlanID, target); err != nil {
		if isPlanNotFound(err) {
			return zero, &codedError{ipc.CodeNotFound, err.Error()}
		}
		return zero, fmt.Errorf("supervisor: set plan status: %w", err)
	}
	return ipc.PlanSetStatusReply{PlanID: args.PlanID, Status: string(target)}, nil
}

// HandleTaskApprove clears the approval gate on a ready_pending_approval task.
func (s *Supervisor) HandleTaskApprove(ctx context.Context, args ipc.TaskApproveArgs) error {
	if args.PlanID == "" {
		return &codedError{ipc.CodeInvalidArgs, "task-approve: plan_id required"}
	}
	if args.TaskID == "" {
		return &codedError{ipc.CodeInvalidArgs, "task-approve: task_id required"}
	}
	found, _, err := s.store.ApproveTask(ctx, args.PlanID, args.TaskID)
	if err != nil {
		return fmt.Errorf("supervisor: approve task: %w", err)
	}
	if !found {
		return &codedError{ipc.CodeNotFound, fmt.Sprintf("task %s/%s not found", args.PlanID, args.TaskID)}
	}
	return nil
}

// HandleWorkerKill cancels the worker's live provider subprocess and then
// reclaims its task and terminates the worker row. The process cancellation
// (orch.KillWorker) aborts the in-flight runner.Run context so the subprocess
// tears down at once rather than running on until its own timeout; the store
// reclaim (kill-and-reclaim, the same shape the reaper uses) requeues the
// task(s) and marks the worker terminated.
func (s *Supervisor) HandleWorkerKill(ctx context.Context, args ipc.WorkerKillArgs) error {
	if args.WorkerID == "" {
		return &codedError{ipc.CodeInvalidArgs, "worker-kill: worker_id required"}
	}
	// Cancel the live provider subprocess first (best-effort): KillWorker aborts
	// the in-flight runner.Run context so exec.CommandContext tears down the
	// process tree, stopping token spend and further checkout mutation. Doing
	// this BEFORE the store reclaim narrows the window in which the requeued
	// task could be re-dispatched while the old process still runs. A false
	// return (no live run under that id) is fine — the store side still applies.
	s.orch.KillWorker(args.WorkerID)

	found, err := s.store.ReclaimWorker(ctx, args.WorkerID)
	if err != nil {
		return fmt.Errorf("supervisor: reclaim worker: %w", err)
	}
	if !found {
		return &codedError{ipc.CodeNotFound, fmt.Sprintf("worker %s not found", args.WorkerID)}
	}
	return nil
}
