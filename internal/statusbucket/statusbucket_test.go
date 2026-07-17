package statusbucket

import (
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestOf_CoversEveryRealStatus is the anti-drift guard: every real
// store.PlanStatus* and store.TaskStatus* value must map to the SPECIFIC bucket
// the product's visual language intends — not silently to the Muted fallback by
// accident. Because both the TUI (statusStyle) and the GUI (theme) derive their
// colour from this one function, pinning the mapping here pins the two surfaces
// together: they cannot drift.
func TestOf_CoversEveryRealStatus(t *testing.T) {
	cases := []struct {
		status string
		want   Bucket
	}{
		// Plan statuses.
		{string(store.PlanStatusDraft), Muted},
		{string(store.PlanStatusActive), Muted}, // "active" is not a task-run state; low-emphasis
		{string(store.PlanStatusPaused), Warn},
		{string(store.PlanStatusDone), Good},
		{string(store.PlanStatusFailedPartial), Warn},
		{string(store.PlanStatusArchived), Muted},
		{string(store.PlanStatusAbandoned), Bad},
		// Task statuses.
		{string(store.TaskStatusPending), Muted},
		{string(store.TaskStatusReady), Muted},
		{string(store.TaskStatusReadyPendingApproval), Warn},
		{string(store.TaskStatusBlocked), Warn},
		{string(store.TaskStatusRunning), Running},
		{string(store.TaskStatusDone), Good},
		{string(store.TaskStatusFailed), Bad},
		{string(store.TaskStatusSkipped), Muted},
		{string(store.TaskStatusDecomposed), Muted},
	}
	for _, c := range cases {
		if got := Of(c.status); got != c.want {
			t.Errorf("Of(%q) = %d, want %d", c.status, got, c.want)
		}
	}
}

// TestOf_UnknownIsMuted confirms a status this package has never heard of falls
// through to the low-emphasis default rather than panicking or mis-colouring.
func TestOf_UnknownIsMuted(t *testing.T) {
	if got := Of("some-future-status"); got != Muted {
		t.Errorf("Of(unknown) = %d, want Muted(%d)", got, Muted)
	}
}
