package tui

import "testing"

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
