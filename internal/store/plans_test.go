package store

import (
	"context"
	"errors"
	"testing"
)

func mustCreateProject(t *testing.T, s *Store, name string) string {
	t.Helper()
	id, err := s.CreateProject(context.Background(), name, []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/tmp/" + name},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return id
}

func TestCreateAndGetPlan(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "plan-project")

	id, err := s.CreatePlan(ctx, CreatePlanOpts{
		ProjectID: projectID,
		Slug:      "my-plan",
		Title:     "My Plan",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	p, err := s.GetPlan(ctx, id)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if p.Slug != "my-plan" || p.Title != "My Plan" || p.ProjectID != projectID {
		t.Errorf("GetPlan = %+v, want slug=my-plan title=%q project=%q", p, "My Plan", projectID)
	}
	if p.Status != PlanStatusDraft {
		t.Errorf("GetPlan.Status = %q, want draft", p.Status)
	}
}

func TestCreatePlanDuplicateSlug(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dup-project")

	if _, err := s.CreatePlan(ctx, CreatePlanOpts{ProjectID: projectID, Slug: "dup", Title: "First"}); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	_, err := s.CreatePlan(ctx, CreatePlanOpts{ProjectID: projectID, Slug: "dup", Title: "Second"})
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("CreatePlan duplicate: err = %v, want ErrDuplicateSlug", err)
	}
}

func TestSetPlanStatusAndListPlans(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "list-project")

	id, err := s.CreatePlan(ctx, CreatePlanOpts{ProjectID: projectID, Slug: "s1", Title: "T1"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := s.SetPlanStatus(ctx, id, PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	plans, err := s.ListPlans(ctx, projectID, nil)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != id {
		t.Errorf("ListPlans = %+v, want one plan with id %q", plans, id)
	}

	if err := s.SetPlanStatus(ctx, id, PlanStatusDone); err != nil {
		t.Fatalf("SetPlanStatus done: %v", err)
	}
	plans, err = s.ListPlans(ctx, projectID, nil)
	if err != nil {
		t.Fatalf("ListPlans after done: %v", err)
	}
	if len(plans) != 0 {
		t.Errorf("ListPlans after done = %+v, want empty (default filter excludes done)", plans)
	}

	plans, err = s.ListPlans(ctx, projectID, []PlanStatus{PlanStatusDone})
	if err != nil {
		t.Fatalf("ListPlans(done): %v", err)
	}
	if len(plans) != 1 {
		t.Errorf("ListPlans(done) = %+v, want one plan", plans)
	}
}

func TestSetPlanStatusNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	err := s.SetPlanStatus(ctx, "nonexistent", PlanStatusActive)
	if err == nil {
		t.Fatal("SetPlanStatus on missing plan: want error, got nil")
	}
	// The drive API relies on this being the typed sentinel (matched with
	// errors.Is) rather than a scraped message, so it can map to CodeNotFound.
	if !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("SetPlanStatus err = %v, want errors.Is ErrPlanNotFound", err)
	}
}

func TestGetPlanNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.GetPlan(ctx, "does-not-exist"); err == nil {
		t.Error("GetPlan for missing plan: want error, got nil")
	}
}

func TestGetPlanFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "getplan-found-project")
	planID := mustCreatePlan(t, s, projectID, "getplan-found-plan")

	got, err := s.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.ID != planID {
		t.Errorf("ID = %q, want %q", got.ID, planID)
	}
	if got.ProjectID != projectID {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, projectID)
	}
}
