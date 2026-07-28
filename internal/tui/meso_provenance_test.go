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

// TestMesoLabelsFanoutPartitionsOnly pins the rendering rule for partitions:
// a marker on every row would be noise, since a partition of one is the
// ordinary case and tells an operator nothing they can act on. The marker
// earns its place only where it says "these rows are ONE fan-out turn".
func TestMesoLabelsFanoutPartitionsOnly(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{
		{ID: "task-1", PlanID: "plan-1", Status: "running", PartitionOrdinal: "aaaa"},
		{ID: "task-2", PlanID: "plan-1", Status: "running", PartitionOrdinal: "aaaa"},
		{ID: "task-3", PlanID: "plan-1", Status: "ready", PartitionOrdinal: "bbbb"},
	}
	out := m.View()

	if !strings.Contains(out, "p1") {
		t.Errorf("the two tasks sharing a partition carry no marker, so an "+
			"operator cannot see they are one fan-out turn:\n%s", out)
	}
	if strings.Contains(out, "p2") {
		t.Errorf("a partition holding ONE task was labelled; singletons are the "+
			"ordinary case and labelling them buries the real fan-out groups:\n%s", out)
	}
}

// TestMesoOmitsPartitionMarkerWithoutOrdinals guards the empty case: tasks
// created without metadata have no ordinal at all, and they must not collapse
// into a shared "" partition that renders as a fan-out group.
func TestMesoOmitsPartitionMarkerWithoutOrdinals(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{
		{ID: "task-1", PlanID: "plan-1", Status: "ready"},
		{ID: "task-2", PlanID: "plan-1", Status: "ready"},
	}
	if out := m.View(); strings.Contains(out, "p1") {
		t.Errorf("tasks with no partition ordinal were labelled as sharing one; "+
			"absent metadata is not evidence of a shared partition:\n%s", out)
	}
}
