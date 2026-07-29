package tui

import (
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// TestTaskDeltaStatusCoversReclaim pins the mapping for the one kind whose
// absence is actively misleading rather than merely late.
//
// The live view applies a delta per event and reconciles on the next poll, so
// an unmapped kind is normally harmless. task.reclaimed is the exception: the
// reaper has just requeued the task because its worker went away, and until the
// poll lands the row still reads 'running' -- naming a worker that no longer
// exists. That is the state an operator is most likely to misread, and it is
// exactly how a reclaimed step was first mistaken for a stuck one.
func TestTaskDeltaStatusCoversReclaim(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want string
	}{
		{"task.reclaimed", "pending"},
		{"task.claimed", "running"},
		{"task.done", "done"},
		{"task.failed", "failed"},
		{"task.released", "ready"},
		{"task.blocked", "blocked"},
		{"worker.heartbeat", ""},
	} {
		if got := taskDeltaStatus(tc.kind); got != tc.want {
			t.Errorf("taskDeltaStatus(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// TestApplyEventReclaimClearsDeadClaim covers the half of the reclaim delta
// that updating Status alone leaves wrong.
//
// The reaper releases the claim when it requeues a task. Setting only Status
// renders `pending` beside `w:<dead-worker>` until the next poll -- naming a
// worker that is definitionally gone, which is worse than the stale `running`
// the mapping was added to fix.
func TestApplyEventReclaimClearsDeadClaim(t *testing.T) {
	snap := snapshot{tasks: []observe.Task{
		{ID: "race", PlanID: "p1", Status: "running", ClaimedByWorkerID: "worker-dead"},
		{ID: "build", PlanID: "p1", Status: "running", ClaimedByWorkerID: "worker-live"},
	}}

	got := applyEvent(snap, ipc.AttachEvent{Kind: "task.reclaimed", TaskID: "race"})

	if got.tasks[0].Status != "pending" {
		t.Errorf("reclaimed task status = %q, want pending", got.tasks[0].Status)
	}
	if got.tasks[0].ClaimedByWorkerID != "" {
		t.Errorf("reclaimed task still claims worker %q; the row would render "+
			"pending beside a worker that no longer exists",
			got.tasks[0].ClaimedByWorkerID)
	}
	// An unrelated task must keep its live claim: clearing broadly would blank
	// the provenance of every running row on any reclaim.
	if got.tasks[1].ClaimedByWorkerID != "worker-live" {
		t.Errorf("an unrelated task lost its claim: %q", got.tasks[1].ClaimedByWorkerID)
	}
}
