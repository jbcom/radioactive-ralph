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

// TestMesoExplainsBlockedTasks closes the same gap a code review found in the
// CLI: a blocked task rendered as a bare status string, which is the one status
// an operator cannot act on without more. A blocked task and one waiting on a
// dependency both sit at zero progress; only one clears itself.
func TestMesoExplainsBlockedTasks(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{
		{
			ID: "task-1", PlanID: "plan-1", Status: "blocked_capability",
			Blocked: &observe.BlockedSummary{
				Category: observe.BlockedCapability,
				Summary:  "bind a provider that does",
			},
		},
		{ID: "task-2", PlanID: "plan-1", Status: "ready"},
	}
	out := m.View()

	if !strings.Contains(out, "bind a provider that does") {
		t.Errorf("blocked task shows no remediation, so the operator cannot tell "+
			"whether it self-clears or needs them to act:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "task-2") && strings.Contains(line, "—") {
			t.Errorf("an unblocked task rendered a blocked-reason separator: %q", line)
		}
	}
}

// visibleWidth is the rune count with ANSI styling removed -- what the terminal
// actually has to fit.
func visibleWidth(line string) int {
	var b strings.Builder
	inEscape := false
	for _, r := range line {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return len([]rune(b.String()))
}

// TestMesoRowsFitAConventionalTerminal pins a defect found by MEASURING the
// rendered output rather than trusting the tests: with every marker present and
// a blocked reason inline, the worst-case row was 215 columns. It wrapped on any
// normal terminal, and because the wrap landed mid-sentence the partition marker
// and the remediation scattered across lines -- unreadable exactly when the
// operator most needs to read it.
//
// The reason now renders on its own continuation line. 120 columns is the bound:
// comfortably past the 80-column default, well inside a maximized modern
// terminal, and tight enough to catch a marker that grows without anyone noticing.
func TestMesoRowsFitAConventionalTerminal(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{{
		ID: "implement-provider-x", PlanID: "plan-1", Status: "blocked_capability",
		ClaimedByWorkerID: "worker-7f3a2b1c", PartitionOrdinal: "aaaa",
		AssignedAlias: "primary-opus",
		Blocked: &observe.BlockedSummary{
			Category: observe.BlockedCapability,
			Summary:  "the bound provider does not satisfy this task's requirements; bind a provider that does",
		},
	}, {
		ID: "peer", PlanID: "plan-1", Status: "running",
		PartitionOrdinal: "aaaa", AssignedAlias: "primary-opus",
	}}
	m.snap.descriptions = map[string]string{
		"implement-provider-x": "wire up the frobnicator end to end",
	}

	for _, line := range strings.Split(m.View(), "\n") {
		if w := visibleWidth(line); w > 120 {
			t.Errorf("row is %d columns and will wrap mid-content:\n%s", w, line)
		}
	}
	// Fitting must not have been achieved by dropping the reason.
	if !strings.Contains(m.View(), "bind a provider that does") {
		t.Error("the remediation vanished; fitting the width must not cost the " +
			"information the line exists to carry")
	}
}

// TestMesoLabelsRunningFanoutPartition exercises the RENDERED path for a
// running partition, which a code review specifically asked for: the store
// keeping the ordinal is necessary but not sufficient, and this repo has
// already seen markers disappear while the suite stayed green.
//
// A running fan-out group is when the marker earns its place -- the operator's
// live question is "is this one turn, or three independent workers?".
func TestMesoLabelsRunningFanoutPartition(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{
		{
			ID: "task-1", PlanID: "plan-1", Status: "running",
			ClaimedByWorkerID: "worker-session-01-7f3a2b1c", PartitionOrdinal: "aaaa",
		},
		{
			ID: "task-2", PlanID: "plan-1", Status: "running",
			ClaimedByWorkerID: "worker-session-01-7f3a2b1c", PartitionOrdinal: "aaaa",
		},
	}
	if out := m.View(); !strings.Contains(out, "p1") {
		t.Errorf("a RUNNING fan-out partition carries no marker, so an operator "+
			"cannot tell one turn from two independent workers:\n%s", out)
	}
}

// TestMesoNamesATerminallyBlockedDependency covers the rendered path for the
// dead-plan marker. The store computing it is necessary but not sufficient --
// this repo has already shipped markers that existed in the DTO and never
// reached a view.
func TestMesoNamesATerminallyBlockedDependency(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{
		{ID: "build", PlanID: "plan-1", Status: "failed"},
		{ID: "race", PlanID: "plan-1", Status: "pending", BlockedByTaskID: "build"},
		{ID: "solo", PlanID: "plan-1", Status: "pending"},
	}
	out := m.View()

	if !strings.Contains(out, "cannot run: build failed") {
		t.Errorf("a task that can never run renders as plain pending:\n%s", out)
	}
	// A task with no terminal blocker must not gain the marker.
	if strings.Count(out, "cannot run") != 1 {
		t.Errorf("the marker appeared on a task with no failed dependency:\n%s", out)
	}
}

// TestMesoShowsDurableFailureCategory closes the CLI/UI asymmetry for the
// third time this session: the CLI explained a failed task and the views did
// not. The TUI showed failures only in its EVENT feed, which is bounded and
// scrolls away, so a long-terminal task's row said nothing.
func TestMesoShowsDurableFailureCategory(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = []observe.Task{
		{ID: "build", PlanID: "plan-1", Status: "failed", FailureCategory: "auth"},
		// A RUNNING task carrying a stale category must not advertise it:
		// status is the authority on what the task is.
		{ID: "retry", PlanID: "plan-1", Status: "running", FailureCategory: "rate_limit"},
	}
	out := m.View()

	if !strings.Contains(out, "failure: auth") {
		t.Errorf("a failed task shows no reason on its row:\n%s", out)
	}
	if strings.Contains(out, "rate_limit") {
		t.Errorf("a RUNNING task advertised a stale failure category:\n%s", out)
	}
}

// TestMacroFlagsAPlanWithNoRunnableWork covers the TUI's plan list. Per the
// render-every-surface rule, a field is not shipped until each renderer shows
// it -- this repo has repeatedly landed one and chased the others later.
func TestMacroFlagsAPlanWithNoRunnableWork(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMacro
	m.snap.plans = []observe.Plan{
		{ID: "p1", Slug: "dead", Title: "Dead plan", Status: "active", NoRunnableWork: true},
		{ID: "p2", Slug: "live", Title: "Live plan", Status: "active"},
	}
	out := m.View()

	if !strings.Contains(out, "no runnable work") {
		t.Errorf("a dead plan carries no marker in the plan list:\n%s", out)
	}
	if strings.Count(out, "no runnable work") != 1 {
		t.Errorf("a healthy plan was flagged too:\n%s", out)
	}
}
