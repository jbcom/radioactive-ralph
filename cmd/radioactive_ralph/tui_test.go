package main

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
)

func TestFormatAcceptanceRendersJSONBullets(t *testing.T) {
	got := formatAcceptance(`["docs updated","tests pass"]`)
	for _, want := range []string{"  - docs updated", "  - tests pass"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatAcceptance() = %q, want substring %q", got, want)
		}
	}
}

func TestRenderTaskDetailIncludesOperatorContext(t *testing.T) {
	detail := renderTaskDetail(taskSummary{
		PlanSlug:        "release",
		TaskID:          "verify",
		Description:     "verify release candidate",
		LatestEventType: "blocked",
		LatestPayload: plandag.TaskEventPayload{
			Reason:       "needs host smoke",
			NeedsContext: []string{"launchd run", "SCM run"},
			Evidence:     []string{"ci green"},
		},
		AcceptanceJSON: `["native host run captured"]`,
		DependsOn:      []string{"package"},
		DependedBy:     []string{"tag"},
	}, lipgloss.NewStyle())

	for _, want := range []string{
		"release/verify",
		"latest_event=blocked",
		"reason=needs host smoke",
		"needs_context=launchd run, SCM run",
		"evidence:",
		"  - ci green",
		"acceptance:",
		"  - native host run captured",
		"depends_on: package",
		"depended_by: tag",
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("renderTaskDetail() missing %q in:\n%s", want, detail)
		}
	}
}

func TestTUIApproveTaskCmdMutatesApprovalTask(t *testing.T) {
	ctx := context.Background()
	repo, store, planID := setupTUIPlan(t, "release")
	defer func() { _ = store.Close() }()

	if err := store.CreateTask(ctx, plandag.CreateTaskOpts{
		PlanID:      planID,
		ID:          "review",
		Description: "review release",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	setTaskStatus(t, store, planID, "review", plandag.TaskStatusReadyPendingApproval)

	msg := approveTaskCmd(repo, taskSummary{
		PlanID:   planID,
		PlanSlug: "release",
		TaskID:   "review",
	})()
	service, ok := msg.(serviceMsg)
	if !ok {
		t.Fatalf("approveTaskCmd returned %T, want serviceMsg", msg)
	}
	if !strings.Contains(service.text, "approved release/review") {
		t.Fatalf("approveTaskCmd text = %q", service.text)
	}
	task, err := store.GetTask(ctx, planID, "review")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != plandag.TaskStatusPending {
		t.Fatalf("task status = %q, want pending", task.Status)
	}
	events, err := store.ListTaskEvents(ctx, planID, "review", 1)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "approved" {
		t.Fatalf("events = %+v, want approved event", events)
	}
	payload, err := plandag.ParseTaskPayload(events[0].PayloadJSON)
	if err != nil {
		t.Fatalf("ParseTaskPayload: %v", err)
	}
	if payload.OperatorAction != "approved" {
		t.Fatalf("operator action = %q, want approved", payload.OperatorAction)
	}
}

func TestTUIHandoffTaskCmdMutatesFailedTask(t *testing.T) {
	ctx := context.Background()
	repo, store, planID := setupTUIPlan(t, "release")
	defer func() { _ = store.Close() }()

	if err := store.CreateTask(ctx, plandag.CreateTaskOpts{
		PlanID:      planID,
		ID:          "smoke",
		Description: "run smoke",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	setTaskStatus(t, store, planID, "smoke", plandag.TaskStatusFailed)

	msg := handoffTaskCmd(repo, taskSummary{
		PlanID:   planID,
		PlanSlug: "release",
		TaskID:   "smoke",
	}, "red: needs incident response")()
	service, ok := msg.(serviceMsg)
	if !ok {
		t.Fatalf("handoffTaskCmd returned %T, want serviceMsg", msg)
	}
	if !strings.Contains(service.text, "handed off release/smoke to red") {
		t.Fatalf("handoffTaskCmd text = %q", service.text)
	}
	task, err := store.GetTask(ctx, planID, "smoke")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != plandag.TaskStatusPending {
		t.Fatalf("task status = %q, want pending", task.Status)
	}
	if task.VariantHint != "red" {
		t.Fatalf("variant hint = %q, want red", task.VariantHint)
	}
	events, err := store.ListTaskEvents(ctx, planID, "smoke", 1)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "requeued" {
		t.Fatalf("events = %+v, want requeued event", events)
	}
	payload, err := plandag.ParseTaskPayload(events[0].PayloadJSON)
	if err != nil {
		t.Fatalf("ParseTaskPayload: %v", err)
	}
	if payload.HandoffTo != "red" || payload.Reason != "needs incident response" || payload.OperatorAction != "handoff" {
		t.Fatalf("payload = %+v, want red handoff with reason", payload)
	}
}

func TestTUIHandoffKeyEntersInputMode(t *testing.T) {
	m := newTUIModel(t.TempDir(), "unused", "unused", false, "")
	m.tab = tabFailed
	m.snapshot.failed = []taskSummary{{PlanID: "p1", PlanSlug: "release", TaskID: "smoke"}}

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	got := updated.(*tuiModel)
	if got.inputMode != inputHandoff {
		t.Fatalf("inputMode = %v, want inputHandoff", got.inputMode)
	}
}

func TestTUIVariantAndProviderFiltersApplyToTaskLists(t *testing.T) {
	m := newTUIModel(t.TempDir(), "unused", "unused", false, "")
	m.snapshot.ready = []taskSummary{
		{PlanSlug: "release", TaskID: "docs", VariantHint: "grey", Provider: "codex"},
		{PlanSlug: "release", TaskID: "smoke", VariantHint: "red", Provider: "gemini"},
	}

	m.cycleVariantFilter()
	if m.variantFilter != "grey" {
		t.Fatalf("variantFilter = %q, want grey", m.variantFilter)
	}
	filtered := m.filteredTasks(m.snapshot.ready)
	if len(filtered) != 1 || filtered[0].TaskID != "docs" {
		t.Fatalf("variant filtered tasks = %+v, want docs only", filtered)
	}

	m.cycleProviderFilter()
	if m.providerFilter != "codex" {
		t.Fatalf("providerFilter = %q, want codex", m.providerFilter)
	}
	filtered = m.filteredTasks(m.snapshot.ready)
	if len(filtered) != 1 || filtered[0].TaskID != "docs" {
		t.Fatalf("combined filtered tasks = %+v, want docs only", filtered)
	}
}

func TestParseHandoffInput(t *testing.T) {
	variantName, reason := parseHandoffInput(" red : needs incident response ")
	if variantName != "red" || reason != "needs incident response" {
		t.Fatalf("parseHandoffInput() = %q, %q", variantName, reason)
	}
}

func setupTUIPlan(t *testing.T, slug string) (string, *plandag.Store, string) {
	t.Helper()
	t.Setenv("RALPH_STATE_DIR", t.TempDir())
	repo := t.TempDir()
	store, err := openPlanStore(context.Background())
	if err != nil {
		t.Fatalf("openPlanStore: %v", err)
	}
	planID, err := store.CreatePlan(context.Background(), plandag.CreatePlanOpts{
		Slug:     slug,
		Title:    "Release",
		RepoPath: repo,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := store.SetPlanStatus(context.Background(), planID, plandag.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}
	return repo, store, planID
}

func setTaskStatus(t *testing.T, store *plandag.Store, planID, taskID string, status plandag.TaskStatus) {
	t.Helper()
	if _, err := store.DB().ExecContext(context.Background(),
		`UPDATE tasks SET status = ? WHERE plan_id = ? AND id = ?`,
		string(status), planID, taskID); err != nil {
		t.Fatalf("set task status: %v", err)
	}
}
