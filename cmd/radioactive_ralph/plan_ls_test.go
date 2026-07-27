package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/observe"
)

type fakePlanSource struct {
	plans   []observe.Plan
	err     error
	gotAll  bool
	gotProj string
}

func (f *fakePlanSource) ListPlans(
	_ context.Context,
	projectID string,
	all bool,
) ([]observe.Plan, error) {
	f.gotProj, f.gotAll = projectID, all
	return f.plans, f.err
}

func samplePlans() []observe.Plan {
	return []observe.Plan{
		{ID: "p1", Slug: "ship-it", Title: "Ship It", Status: "active", TaskDone: 1, TaskTotal: 2},
		{ID: "p2", Slug: "clean-up", Title: "Clean Up", Status: "paused"},
	}
}

// TestPlanLsJSONIsOneObjectPerLine pins the machine-readable contract issue
// #204 asks for: a caller must be able to stream results line by line rather
// than buffering a whole array, and an empty result must be zero lines rather
// than a bare "[]" every consumer has to special-case.
func TestPlanLsJSONIsOneObjectPerLine(t *testing.T) {
	src := &fakePlanSource{plans: samplePlans()}
	var out bytes.Buffer
	if err := runPlanLsWith(t.Context(), &out, src, "proj-1", false, true); err != nil {
		t.Fatalf("runPlanLsWith: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per plan:\n%s", len(lines), out.String())
	}
	var first observe.Plan
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if first.Slug != "ship-it" || first.Status != "active" || first.TaskTotal != 2 {
		t.Errorf("decoded %+v, want the full plan record", first)
	}
}

func TestPlanLsJSONEmitsNothingForNoPlans(t *testing.T) {
	src := &fakePlanSource{}
	var out bytes.Buffer
	if err := runPlanLsWith(t.Context(), &out, src, "proj-1", false, true); err != nil {
		t.Fatalf("runPlanLsWith: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("JSON mode emitted %q for zero plans, want nothing", out.String())
	}
}

// TestPlanLsHumanOutputUnchanged keeps the default view exactly as operators
// know it — adding --json must not alter the text shape.
func TestPlanLsHumanOutputUnchanged(t *testing.T) {
	src := &fakePlanSource{plans: samplePlans()}
	var out bytes.Buffer
	if err := runPlanLsWith(t.Context(), &out, src, "proj-1", false, false); err != nil {
		t.Fatalf("runPlanLsWith: %v", err)
	}
	got := out.String()
	for _, want := range []string{"active", "ship-it", "Ship It", "paused", "clean-up", "Clean Up"} {
		if !strings.Contains(got, want) {
			t.Errorf("human output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "{") {
		t.Errorf("human output leaked JSON:\n%s", got)
	}
}

func TestPlanLsHumanOutputReportsNoPlans(t *testing.T) {
	src := &fakePlanSource{}
	var out bytes.Buffer
	if err := runPlanLsWith(t.Context(), &out, src, "proj-1", false, false); err != nil {
		t.Fatalf("runPlanLsWith: %v", err)
	}
	if !strings.Contains(out.String(), "no plans") {
		t.Fatalf("expected a no-plans notice, got %q", out.String())
	}
}

// TestPlanLsPropagatesAllFlagAndProject proves the scope reaches the source
// rather than being silently dropped.
func TestPlanLsPropagatesAllFlagAndProject(t *testing.T) {
	src := &fakePlanSource{plans: samplePlans()}
	var out bytes.Buffer
	if err := runPlanLsWith(t.Context(), &out, src, "proj-42", true, false); err != nil {
		t.Fatalf("runPlanLsWith: %v", err)
	}
	if !src.gotAll {
		t.Error("--all did not reach the source")
	}
	if src.gotProj != "proj-42" {
		t.Errorf("project = %q, want proj-42", src.gotProj)
	}
}

// TestPlanLsSurfacesSourceError fails loudly rather than printing an empty
// list, which would read as "this project has no plans".
func TestPlanLsSurfacesSourceError(t *testing.T) {
	src := &fakePlanSource{err: errors.New("supervisor unreachable")}
	var out bytes.Buffer
	err := runPlanLsWith(t.Context(), &out, src, "proj-1", false, false)
	if err == nil {
		t.Fatal("a source failure must not render as an empty plan list")
	}
	if !strings.Contains(err.Error(), "supervisor unreachable") {
		t.Errorf("error = %v, want it to surface the cause", err)
	}
}

// pagedPlans turns a list of pages into a fetch func, recording how many pages
// were actually requested.
func pagedPlans(pages []observe.PlanPage, requested *int) func(string) (observe.PlanPage, error) {
	return func(afterID string) (observe.PlanPage, error) {
		*requested++
		if afterID == "" {
			return pages[0], nil
		}
		for i, p := range pages {
			if i > 0 && pages[i-1].NextAfterID == afterID {
				return p, nil
			}
		}
		return observe.PlanPage{}, nil
	}
}

// TestPlanLsPaginatesToExhaustion is the P1. The snapshot caps a page at
// MaxPageLimit, and this command FILTERS after fetching — so reading only the
// first page could print "no plans" while an active plan sat on page two.
// Filtering a truncated page is strictly worse than truncating a filtered one.
func TestPlanLsPaginatesToExhaustion(t *testing.T) {
	requested := 0
	pages := []observe.PlanPage{
		{
			Items:       []observe.Plan{{ID: "p1", Slug: "first", Status: "done"}},
			HasMore:     true,
			NextAfterID: "p1",
		},
		{
			Items: []observe.Plan{{ID: "p2", Slug: "second", Status: "active"}},
		},
	}

	got, err := collectPlanPages(context.Background(), false, pagedPlans(pages, &requested))
	if err != nil {
		t.Fatalf("collectPlanPages: %v", err)
	}
	if requested != 2 {
		t.Fatalf("requested %d page(s), want 2 — HasMore/NextAfterID must be followed", requested)
	}
	if len(got) != 1 || got[0].Slug != "second" {
		t.Fatalf("got %+v, want the active plan that lives on page TWO — stopping "+
			"after one page reports 'no plans' while active work exists", got)
	}
}

// TestPlanLsKeepsDraftsBehindAll pins the default view. The command's previous
// default was active+paused, and its --all help still describes --all as what
// widens beyond that — so including drafts by default silently changed what an
// operator sees without any flag change.
func TestPlanLsKeepsDraftsBehindAll(t *testing.T) {
	page := []observe.PlanPage{{Items: []observe.Plan{
		{ID: "a", Slug: "act", Status: "active"},
		{ID: "d", Slug: "draft", Status: "draft"},
		{ID: "p", Slug: "paused", Status: "paused"},
		{ID: "x", Slug: "done", Status: "done"},
	}}}

	requested := 0
	got, err := collectPlanPages(context.Background(), false, pagedPlans(page, &requested))
	if err != nil {
		t.Fatalf("collectPlanPages: %v", err)
	}
	for _, plan := range got {
		if plan.Status == "draft" {
			t.Fatalf("default view included a draft: %+v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("default view = %+v, want active + paused only", got)
	}

	requested = 0
	all, err := collectPlanPages(context.Background(), true, pagedPlans(page, &requested))
	if err != nil {
		t.Fatalf("collectPlanPages(all): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("--all = %+v, want every status", all)
	}
}

// TestPlanLsOrdersMostRecentlyUpdatedFirst pins the ordering. The snapshot
// endpoint orders by plan id; this command has always shown most-recently
// touched first, so an operator who just paused a plan expects it at the top
// rather than wherever its id happens to sort.
func TestPlanLsOrdersMostRecentlyUpdatedFirst(t *testing.T) {
	older := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	// Deliberately id-ascending so an unsorted implementation returns the
	// STALE plan first.
	page := []observe.PlanPage{{Items: []observe.Plan{
		{ID: "aaa", Slug: "stale", Status: "active", UpdatedAt: older},
		{ID: "zzz", Slug: "fresh", Status: "active", UpdatedAt: newer},
	}}}

	requested := 0
	got, err := collectPlanPages(context.Background(), false, pagedPlans(page, &requested))
	if err != nil {
		t.Fatalf("collectPlanPages: %v", err)
	}
	if len(got) != 2 || got[0].Slug != "fresh" {
		t.Fatalf("order = %+v, want the most recently updated plan first", got)
	}
}
