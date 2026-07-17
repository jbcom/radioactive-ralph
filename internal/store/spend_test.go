package store

import (
	"context"
	"testing"
)

func TestRecordSpendAndProjectSpendByProvider(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "spend-project")
	_, workerID := mustCreateSessionAndWorker(t, s, "spend")

	if err := s.RecordSpend(ctx, RecordSpendOpts{
		ProjectID: projectID, WorkerID: workerID, Provider: "claude", Model: "sonnet",
		InputTokens: 100, OutputTokens: 50, CachedTokens: 10, CostUSD: 1.25,
	}); err != nil {
		t.Fatalf("RecordSpend: %v", err)
	}
	if err := s.RecordSpend(ctx, RecordSpendOpts{
		ProjectID: projectID, Provider: "claude", CostUSD: 0.75,
	}); err != nil {
		t.Fatalf("RecordSpend: %v", err)
	}
	if err := s.RecordSpend(ctx, RecordSpendOpts{
		ProjectID: projectID, Provider: "codex", CostUSD: 3.00,
	}); err != nil {
		t.Fatalf("RecordSpend: %v", err)
	}

	totals, err := s.ProjectSpendByProvider(ctx, projectID)
	if err != nil {
		t.Fatalf("ProjectSpendByProvider: %v", err)
	}
	if got := totals["claude"]; got != 2.00 {
		t.Errorf("claude total = %v, want 2.00", got)
	}
	if got := totals["codex"]; got != 3.00 {
		t.Errorf("codex total = %v, want 3.00", got)
	}
}

func TestProjectSpendByProviderEmptyForFreshProject(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "spend-fresh-project")

	totals, err := s.ProjectSpendByProvider(ctx, projectID)
	if err != nil {
		t.Fatalf("ProjectSpendByProvider: %v", err)
	}
	if len(totals) != 0 {
		t.Errorf("totals = %+v, want empty", totals)
	}
}

func TestRecordSpendRequiresFields(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.RecordSpend(ctx, RecordSpendOpts{}); err == nil {
		t.Fatal("expected an error for an empty RecordSpendOpts")
	}
}
