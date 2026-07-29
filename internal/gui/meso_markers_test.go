//go:build gui

package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// renderedText collects every human-visible string in a widget tree. It walks
// canvas.Text as well as the widgets: the status chip is a canvas.Text, and a
// walker that skips it reports a row as empty when it is not -- which is
// exactly the false conclusion a first pass at this drew.
func renderedText(obj fyne.CanvasObject, out *strings.Builder) {
	switch o := obj.(type) {
	case *widget.Button:
		out.WriteString(o.Text + "\n")
	case *widget.Label:
		out.WriteString(o.Text + "\n")
	case *canvas.Text:
		out.WriteString(o.Text + "\n")
	case *fyne.Container:
		for _, c := range o.Objects {
			renderedText(c, out)
		}
	case *container.Scroll:
		renderedText(o.Content, out)
	}
}

func mesoText(t *testing.T, tasks []observe.Task) string {
	t.Helper()
	f := newFakeController()
	u := newTestUI(t, f)
	u.render(snapshot{level: levelMeso, selectedPlan: "p1", tasks: tasks})
	var b strings.Builder
	renderedText(u.root, &b)
	return b.String()
}

// TestMesoShowsBlockedReasonAndPartition pins the two markers the GUI gained
// alongside the TUI and CLI. The GUI holds []observe.Task straight off the IPC
// reply, so Blocked reaches it in production even though the fake controller
// (which deals in store.Task) never populates it -- meaning no existing test
// covered this path at all.
func TestMesoShowsBlockedReasonAndPartition(t *testing.T) {
	got := mesoText(t, []observe.Task{
		{
			PlanID: "p1", ID: "task-a", Status: "blocked_capability",
			PartitionOrdinal: "aaaa",
			Blocked: &observe.BlockedSummary{
				Category: observe.BlockedCapability,
				Summary:  "bind a provider that does",
			},
		},
		{PlanID: "p1", ID: "task-b", Status: "running", PartitionOrdinal: "aaaa"},
		{PlanID: "p1", ID: "task-c", Status: "ready", PartitionOrdinal: "bbbb"},
	})

	if !strings.Contains(got, "bind a provider that does") {
		t.Errorf("blocked task shows no remediation, so an operator cannot tell "+
			"whether it self-clears or needs them to act:\n%s", got)
	}
	if !strings.Contains(got, "p1") {
		t.Errorf("the two tasks sharing a partition carry no marker:\n%s", got)
	}
	if strings.Contains(got, "p2") {
		t.Errorf("a partition of one was labelled; singletons are the ordinary "+
			"case and marking them buries the real fan-out groups:\n%s", got)
	}
}

// findWrappedLabel returns the first Label containing want whose Wrapping is
// enabled, or nil.
func findWrappedLabel(obj fyne.CanvasObject, want string) *widget.Label {
	switch o := obj.(type) {
	case *widget.Label:
		if strings.Contains(o.Text, want) && o.Wrapping != fyne.TextWrapOff {
			return o
		}
	case *fyne.Container:
		for _, c := range o.Objects {
			if l := findWrappedLabel(c, want); l != nil {
				return l
			}
		}
	case *container.Scroll:
		return findWrappedLabel(o.Content, want)
	}
	return nil
}

// TestMesoBlockedReasonIsReachable pins the STRUCTURE, not just the presence of
// the text -- a review caught that the two are different.
//
// The remediation first shipped as another cell in the task's HBox. The body
// sits in a VERTICAL-only scroll (newUI builds NewVScroll), so at the default
// window width the end of a long sentence was clipped with no way to scroll to
// it: the operator could see a block existed but not what to do about it. The
// text-presence assertion above passed the whole time.
func TestMesoBlockedReasonIsReachable(t *testing.T) {
	f := newFakeController()
	u := newTestUI(t, f)
	u.render(snapshot{level: levelMeso, selectedPlan: "p1", tasks: []observe.Task{{
		PlanID: "p1", ID: "task-a", Status: "blocked_capability",
		Blocked: &observe.BlockedSummary{
			Category: observe.BlockedCapability,
			Summary:  "the bound provider does not satisfy this task's requirements; bind a provider that does",
		},
	}}})

	if findWrappedLabel(u.root, "bind a provider that does") == nil {
		t.Error("the remediation is not in a wrapping label; inside the task row " +
			"it is clipped at the window edge under a vertical-only scroll, so " +
			"the operator cannot read the action they need")
	}
}

// TestMesoShowsWorkerClaim closes a gap found by dumping the widget tree: the
// TUI and CLI both render the worker claim and the GUI did not, so the same
// task read differently depending on which surface an operator opened.
//
// The claim answers a different question from the partition -- two tasks can
// share a ready partition and still be held by different workers -- so the
// partition label cannot stand in for it.
func TestMesoShowsWorkerClaim(t *testing.T) {
	got := mesoText(t, []observe.Task{
		{
			PlanID: "p1", ID: "task-a", Status: "running",
			ClaimedByWorkerID: "worker-session-01-7f3a2b1c", PartitionOrdinal: "aaaa",
		},
		{
			PlanID: "p1", ID: "task-b", Status: "running",
			ClaimedByWorkerID: "worker-session-01-9e8d7c6b", PartitionOrdinal: "aaaa",
		},
		{PlanID: "p1", ID: "task-c", Status: "ready"},
	})

	if !strings.Contains(got, "w:…7f3a2b1c") || !strings.Contains(got, "w:…9e8d7c6b") {
		t.Errorf("worker claims missing or indistinguishable; two tasks sharing a "+
			"partition can still be held by different workers:\n%s", got)
	}
	if strings.Count(got, "w:") != 2 {
		t.Errorf("an unclaimed task gained a worker marker:\n%s", got)
	}
}

// TestMesoNamesATerminallyBlockedDependency covers the GUI's dead-plan marker.
// It is a wrapping row for the same reason the block remediation is: inside
// the task HBox, under a vertical-only scroll, a long line is clipped with no
// way to reach it.
func TestMesoNamesATerminallyBlockedDependency(t *testing.T) {
	got := mesoText(t, []observe.Task{
		{PlanID: "p1", ID: "build", Status: "failed"},
		{PlanID: "p1", ID: "race", Status: "pending", BlockedByTaskID: "build"},
		{PlanID: "p1", ID: "solo", Status: "pending"},
	})
	if !strings.Contains(got, "cannot run: build failed") {
		t.Errorf("a task that can never run renders as plain pending:\n%s", got)
	}
	if strings.Count(got, "cannot run") != 1 {
		t.Errorf("the marker appeared on a task with no failed dependency:\n%s", got)
	}
}

// TestMesoShowsDurableFailureCategory mirrors the TUI and CLI: a failed task
// explains itself from the durable category, and a running task carrying a
// stale one does not.
func TestMesoShowsDurableFailureCategory(t *testing.T) {
	got := mesoText(t, []observe.Task{
		{PlanID: "p1", ID: "build", Status: "failed", FailureCategory: "auth"},
		{PlanID: "p1", ID: "retry", Status: "running", FailureCategory: "rate_limit"},
	})
	if !strings.Contains(got, "failure: auth") {
		t.Errorf("a failed task shows no reason on its row:\n%s", got)
	}
	if strings.Contains(got, "rate_limit") {
		t.Errorf("a RUNNING task advertised a stale failure category:\n%s", got)
	}
}

// TestMacroFlagsAPlanWithNoRunnableWork completes the three-surface coverage
// for the dead-plan signal.
func TestMacroFlagsAPlanWithNoRunnableWork(t *testing.T) {
	f := newFakeController()
	u := newTestUI(t, f)
	u.render(snapshot{level: levelMacro, plans: []observe.Plan{
		{ID: "p1", Slug: "dead", Title: "Dead plan", Status: "active", NoRunnableWork: true},
		{ID: "p2", Slug: "live", Title: "Live plan", Status: "active"},
	}})
	var b strings.Builder
	renderedText(u.root, &b)
	got := b.String()

	if !strings.Contains(got, "no runnable work") {
		t.Errorf("a dead plan carries no marker in the plan list:\n%s", got)
	}
	if strings.Count(got, "no runnable work") != 1 {
		t.Errorf("a healthy plan was flagged too:\n%s", got)
	}
}

// TestMesoShowsReclaimReason completes the three-surface coverage for the
// reclaim cause.
//
// A field that reaches the store and the CLI but not the TUI or GUI leaves two
// of three operator surfaces unable to explain the reclaim -- the gap a review
// caught on this very change, against the repository's own all-surfaces rule.
//
// NOT status-gated, unlike the failure category directly above: a reclaim
// returns the task to pending and it may be running again by the time anyone
// looks, so a running task legitimately shows its reclaim history.
func TestMesoShowsReclaimReason(t *testing.T) {
	got := mesoText(t, []observe.Task{
		{PlanID: "p1", ID: "race", Status: "running", ReclaimCount: 2, ReclaimReason: "stale_heartbeat", ReclaimConcurrentClaims: 6},
		{PlanID: "p1", ID: "build", Status: "done"},
	})
	if !strings.Contains(got, "reclaimed 2x") {
		t.Errorf("a reclaimed task does not state its count:\n%s", got)
	}
	if !strings.Contains(got, "stale_heartbeat") {
		t.Errorf("a reclaimed task does not name its cause:\n%s", got)
	}
	if strings.Count(got, "reclaimed") != 1 {
		t.Errorf("a task that was never reclaimed carries a reclaim marker:\n%s", got)
	}
	if !strings.Contains(got, "6 claims in flight") {
		t.Errorf("a reclaim under load does not name the load:\n%s", got)
	}
}

// TestMesoShowsAttemptLabel mirrors the CLI and TUI.
func TestMesoShowsAttemptLabel(t *testing.T) {
	both := mesoText(t, []observe.Task{
		{PlanID: "p1", ID: "flaky", Status: "running", ReclaimCount: 2,
			ReclaimReason: "stale_heartbeat", RetryCount: 1},
	})
	if !strings.Contains(both, "4 attempts") {
		t.Errorf("1 retry + 2 reclaims does not state its 4 claims:\n%s", both)
	}
	clean := mesoText(t, []observe.Task{{PlanID: "p1", ID: "fine", Status: "done"}})
	if strings.Contains(clean, "attempts") {
		t.Errorf("an untroubled task carries an attempt marker:\n%s", clean)
	}
}
