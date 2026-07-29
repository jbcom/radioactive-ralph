package observe

import "testing"

// TestAttemptLabel pins the display policy every renderer now shares.
//
// The first version duplicated this arithmetic and its visibility gate across
// the CLI, TUI, and GUI. A reviewer flagged that as certain to drift, which the
// comment above PartitionLabels had already predicted in general terms: two
// copies of a display rule diverge the first time either is touched.
//
// It also had the arithmetic wrong. RetryCount and ReclaimCount both count
// claims the task LOST; neither counts the one it holds now. Summing only those
// reported 3 for a task that had had 4 claims, and 0 for a task succeeding on
// its first -- a number no reader would recognise as a count of anything.
func TestAttemptLabel(t *testing.T) {
	for _, tc := range []struct {
		name string
		task Task
		want string
	}{
		{
			name: "never lost a claim",
			task: Task{Status: "done"},
			want: "", // "1 attempt" is true of every healthy row and marks nothing.
		},
		{
			name: "one requeued failure, re-claimed and running",
			task: Task{Status: "running", RetryCount: 1},
			want: "2 attempts",
		},
		{
			// THE REQUEUE WINDOW. MarkFailedWithPayload leaves a retried task
			// `pending` with its claim CLEARED, so it holds nothing -- its claims
			// are exactly the ones it lost. An unconditional +1 advertised three
			// attempts after two, and that window stays visible while dispatch
			// capacity is occupied.
			name: "requeued but not yet re-claimed",
			task: Task{Status: "pending", RetryCount: 2},
			want: "2 attempts",
		},
		{
			name: "finished on its claim",
			task: Task{Status: "done", RetryCount: 1},
			want: "2 attempts",
		},
		{
			name: "reclaims only, running again",
			task: Task{Status: "running", ReclaimCount: 2},
			// "reclaimed 2x" says claims LOST; this says claims GIVEN. Different
			// numbers, and the reader should not have to add them.
			want: "3 attempts",
		},
		{
			name: "both counters moved",
			task: Task{Status: "running", RetryCount: 1, ReclaimCount: 2},
			want: "4 attempts",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AttemptLabel(tc.task); got != tc.want {
				t.Errorf("AttemptLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
