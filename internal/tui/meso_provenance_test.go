package tui

import (
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// TestTaskProvenanceLabelPrefersAlias pins the precedence the meso row depends
// on. Two pool entries can share a provider type while differing in model or
// effort, so labelling by type would render them identically and defeat the
// point of showing provenance at all.
func TestTaskProvenanceLabelPrefersAlias(t *testing.T) {
	for name, tc := range map[string]struct {
		task observe.Task
		want string
	}{
		"alias wins over provider": {
			observe.Task{AssignedAlias: "primary", AssignedProvider: "codex"}, "primary",
		},
		"falls back to provider": {
			observe.Task{AssignedProvider: "codex"}, "codex",
		},
		"unexecuted task has no label": {
			observe.Task{}, "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.task.ProvenanceLabel(); got != tc.want {
				t.Fatalf("taskProvenanceLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMesoRowShowsProvenanceOnlyWhenRecorded checks the rendered row rather
// than the helper alone: the helper returning "" is only useful if the caller
// then omits the marker entirely. Emitting a bare "via=" for an unexecuted task
// would read as a provider whose name failed to render.
func TestMesoRowShowsProvenanceOnlyWhenRecorded(t *testing.T) {
	render := func(t *testing.T, task observe.Task) string {
		t.Helper()
		f := testFake()
		m := newTestModel(t, f)
		m.lvl = levelMeso
		m.selectedPlan = f.plans[0]
		m.snap.tasks = []observe.Task{task}
		return m.View()
	}

	ran := observe.Task{ID: "task-1", PlanID: "plan-1", Status: "running", AssignedAlias: "primary"}
	if got := render(t, ran); !strings.Contains(got, "via=primary") {
		t.Errorf("meso row for an executed task lacks its provenance:\n%s", got)
	}

	pending := observe.Task{ID: "task-2", PlanID: "plan-1", Status: "ready"}
	if got := render(t, pending); strings.Contains(got, "via=") {
		t.Errorf("meso row for an unexecuted task shows a provenance marker:\n%s\n"+
			"a task that never ran must render as unassigned, not as an empty provider",
			got)
	}
}
