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
