package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// TestMesoRendersTaskDescription pins that the operator sees the label they
// wrote. The bulk snapshot deliberately withholds descriptions (they are
// author-controlled free text that can carry filesystem paths), so the views
// fetch them through the opt-in TaskDetail query; without that wiring the task
// list degrades to bare ids and is effectively unusable.
func TestMesoRendersTaskDescription(t *testing.T) {
	f := testFake()
	f.descriptions = map[string]string{"task-a": "wire up the frobnicator"}
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = f.tasksByPlan["plan-1"]
	m.snap.descriptions = map[string]string{"task-a": "wire up the frobnicator"}

	if out := m.View(); !strings.Contains(out, "wire up the frobnicator") {
		t.Fatalf("meso view is missing the task description:\n%s", out)
	}
}

// TestMicroRendersTaskDescription covers the drilled-in header.
func TestMicroRendersTaskDescription(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMicro
	m.selectedPlan = f.plans[0]
	m.selectedTask = observe.Task{ID: "task-a", PlanID: "plan-1", Status: "running"}
	m.snap.descriptions = map[string]string{"task-a": "wire up the frobnicator"}

	if out := m.View(); !strings.Contains(out, "wire up the frobnicator") {
		t.Fatalf("micro view is missing the task description:\n%s", out)
	}
}

// TestDescriptionFetchFailureDegradesToIDs is the resilience property. A label
// is cosmetic, so a supervisor that cannot serve TaskDetail — an older one
// answering CodeUnsupportedCommand, say — must leave the view working rather
// than blanking it.
func TestDescriptionFetchFailureDegradesToIDs(t *testing.T) {
	f := testFake()
	f.detailErr = errors.New("unsupported command")

	got := fetchDescriptions(t.Context(), f, "project-1", "plan-1", []string{"task-a", "task-b"})
	if len(got) != 0 {
		t.Fatalf("descriptions = %v, want empty on fetch failure", got)
	}

	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = f.tasksByPlan["plan-1"]
	m.snap.descriptions = got

	out := m.View()
	if !strings.Contains(out, "task-a") {
		t.Fatalf("meso view must still list tasks by id when descriptions are unavailable:\n%s", out)
	}
}

// TestFetchDescriptionsSkipsBlankPlan avoids a pointless round trip when there
// is no plan in scope. The same guard covers an empty task list, so a page with
// nothing on it costs no query.
func TestFetchDescriptionsSkipsBlankPlan(t *testing.T) {
	f := testFake()
	f.descriptions = map[string]string{"task-a": "real label"}
	if got := fetchDescriptions(t.Context(), f, "project-1", "", []string{"task-a"}); got != nil {
		t.Fatalf("descriptions = %v, want nil for an empty plan id", got)
	}
}

// TestFetchDescriptionsCostsOneRoundTrip pins the per-plan contract. Fetching
// one label per task would cost N round trips inside a single refresh budget —
// each a separate socket dial over IPC — which is what starved the drill-in.
func TestFetchDescriptionsCostsOneRoundTrip(t *testing.T) {
	f := testFake()
	f.descriptions = map[string]string{"task-a": "first", "task-b": "second"}
	got := fetchDescriptions(t.Context(), f, "project-1", "plan-1", []string{"task-a", "task-b"})
	if len(got) != 2 || got["task-a"] != "first" || got["task-b"] != "second" {
		t.Fatalf("descriptions = %v, want both labels from one call", got)
	}
	if f.descriptionCalls != 1 {
		t.Fatalf("TaskDescriptions called %d times, want exactly 1 for a whole plan", f.descriptionCalls)
	}
}

// TestFetchedDescriptionsSurviveTheMerge closes the gap that made the e2e views
// render an empty label column: handleFetched merges the snapshot field by
// field, so a new field that is fetched but never merged is silently dropped.
// The unit render tests all passed while this was broken, because they set
// snap.descriptions directly instead of going through a fetch.
func TestFetchedDescriptionsSurviveTheMerge(t *testing.T) {
	f := testFake()
	f.descriptions = map[string]string{"task-a": "wire up the frobnicator"}
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]

	msg := m.fetchCmd()()
	fetched, ok := msg.(fetchedMsg)
	if !ok {
		t.Fatalf("fetchCmd returned %T, want fetchedMsg", msg)
	}
	if fetched.snap.descriptions["task-a"] == "" {
		t.Fatal("the fetch itself lost the description")
	}

	updated, _ := m.Update(fetched)
	m = updated.(Model)
	if m.snap.descriptions["task-a"] != "wire up the frobnicator" {
		t.Fatalf("descriptions did not survive handleFetched: %v", m.snap.descriptions)
	}
	if out := m.View(); !strings.Contains(out, "wire up the frobnicator") {
		t.Fatalf("meso view lacks the description after a real fetch:\n%s", out)
	}
}

// TestFetchDescriptionsSkipsEmptyTaskList proves the read is bounded by the
// visible page: with no tasks to render there is nothing to look up, and a
// query would be a pure waste on every one-second refresh.
func TestFetchDescriptionsSkipsEmptyTaskList(t *testing.T) {
	f := testFake()
	f.descriptions = map[string]string{"task-a": "label"}
	if got := fetchDescriptions(t.Context(), f, "project-1", "plan-1", nil); got != nil {
		t.Fatalf("descriptions = %v, want nil for an empty task page", got)
	}
	if f.descriptionCalls != 0 {
		t.Fatalf("issued %d queries for an empty page, want 0", f.descriptionCalls)
	}
}

// TestFetchDescriptionsRequestsOnlyVisibleTasks pins the bound itself: the
// query must name the rendered tasks, not the whole plan.
func TestFetchDescriptionsRequestsOnlyVisibleTasks(t *testing.T) {
	f := testFake()
	f.descriptions = map[string]string{"task-a": "first"}
	fetchDescriptions(t.Context(), f, "project-1", "plan-1", []string{"task-a"})
	if len(f.gotTaskIDs) != 1 || f.gotTaskIDs[0] != "task-a" {
		t.Fatalf("query carried task ids %v, want exactly [task-a]", f.gotTaskIDs)
	}
}
