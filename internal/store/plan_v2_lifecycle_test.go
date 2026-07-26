package store

import (
	"context"
	"errors"
	"testing"
)

func mustCreateActiveV2Task(
	t *testing.T,
	s *Store,
	slug string,
) (planID, sessionID, workerID string) {
	t.Helper()
	ctx := context.Background()
	projectID := mustCreateProject(t, s, slug+"-project")
	var err error
	planID, err = s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan:   CreatePlanOpts{ProjectID: projectID, Slug: slug, Title: slug},
		Status: PlanStatusActive,
		Tasks: []GraphTaskSpec{{
			ID: "task", Description: "work", TeamPath: "studio", MetadataJSON: `{}`,
		}},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}
	sessionID, workerID = mustCreateSessionAndWorker(t, s, slug)
	if _, err := s.ClaimReadyTask(ctx, planID, "task", sessionID, workerID); err != nil {
		t.Fatalf("ClaimReadyTask: %v", err)
	}
	return planID, sessionID, workerID
}

func TestTaskProvenanceRejectsStaleClaimingSession(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	planID, sessionA, _ := mustCreateActiveV2Task(t, s, "stale-provenance")

	if err := s.RecordTaskExecution(
		ctx, planID, "task", "alias-a", "provider-a", "model-a", "effort-a",
		"domain-a", sessionA,
	); err != nil {
		t.Fatalf("RecordTaskExecution A: %v", err)
	}
	if err := s.RecordTaskProviderSession(
		ctx, planID, "task", sessionA, "provider-session-a",
	); err != nil {
		t.Fatalf("RecordTaskProviderSession A: %v", err)
	}
	if err := s.ReleaseClaim(ctx, planID, "task", sessionA, "simulate reclaim"); err != nil {
		t.Fatalf("ReleaseClaim A: %v", err)
	}
	sessionB, workerB := mustCreateSessionAndWorker(t, s, "stale-provenance-b")
	if _, err := s.ClaimReadyTask(ctx, planID, "task", sessionB, workerB); err != nil {
		t.Fatalf("ClaimReadyTask B: %v", err)
	}

	if err := s.RecordTaskExecution(
		ctx, planID, "task", "stale-alias", "stale-provider", "stale-model",
		"stale-effort", "stale-domain", sessionA,
	); !errors.Is(err, ErrTaskNotOwnedRunning) {
		t.Fatalf("stale RecordTaskExecution error = %v, want ErrTaskNotOwnedRunning", err)
	}
	if err := s.RecordTaskExecution(
		ctx, planID, "task", "alias-b", "provider-b", "model-b", "effort-b",
		"domain-b", sessionB,
	); err != nil {
		t.Fatalf("RecordTaskExecution B: %v", err)
	}
	metadata, err := s.GetTaskExecutionMetadata(ctx, planID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ProviderSessionID != "" {
		t.Fatalf("new owner inherited stale provider session %q", metadata.ProviderSessionID)
	}
	if err := s.RecordTaskExecution(
		ctx, planID, "task", "conflict-alias", "provider-b", "model-b", "effort-b",
		"domain-b", sessionB,
	); !errors.Is(err, ErrTaskExecutionConflict) {
		t.Fatalf("conflicting same-owner execution error = %v, want ErrTaskExecutionConflict", err)
	}
	if err := s.RecordTaskProviderSession(
		ctx, planID, "task", sessionA, "stale-provider-session",
	); !errors.Is(err, ErrTaskNotOwnedRunning) {
		t.Fatalf("stale RecordTaskProviderSession error = %v, want ErrTaskNotOwnedRunning", err)
	}
	if err := s.RecordTaskProviderSession(
		ctx, planID, "task", sessionB, "provider-session-b",
	); err != nil {
		t.Fatalf("RecordTaskProviderSession B: %v", err)
	}
	if err := s.RecordTaskProviderSession(
		ctx, planID, "task", sessionB, "provider-session-conflict",
	); !errors.Is(err, ErrTaskProviderSessionConflict) {
		t.Fatalf(
			"conflicting same-owner provider session error = %v, want ErrTaskProviderSessionConflict",
			err,
		)
	}
	if err := s.RecordTaskProviderSession(
		ctx, planID, "task", sessionB, "provider-session-b",
	); err != nil {
		t.Fatalf("idempotent RecordTaskProviderSession B: %v", err)
	}
	if err := s.RecordTaskExecution(
		ctx, planID, "task", "alias-b", "provider-b", "model-b", "effort-b",
		"domain-b", sessionB,
	); err != nil {
		t.Fatalf("idempotent RecordTaskExecution B: %v", err)
	}
	metadata, err = s.GetTaskExecutionMetadata(ctx, planID, "task")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if metadata.AssignedAlias != "alias-b" ||
		metadata.AssignedProvider != "provider-b" ||
		metadata.AssignedSessionID != sessionB ||
		metadata.ProviderSessionID != "provider-session-b" {
		t.Fatalf("current owner provenance was overwritten: %+v", metadata)
	}
}

func TestTerminalFailureConvergesActiveV2PlanToFailedPartial(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	planID, sessionID, _ := mustCreateActiveV2Task(t, s, "terminal-failure")

	retried, err := s.MarkFailed(ctx, planID, "task", sessionID, "terminal", 0)
	if err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if retried {
		t.Fatal("terminal MarkFailed retried")
	}
	plan, err := s.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if plan.Status != PlanStatusFailedPartial {
		t.Fatalf("plan status = %s, want %s", plan.Status, PlanStatusFailedPartial)
	}
}

func TestLateCompletionPreservesOperatorOwnedPlanStatus(t *testing.T) {
	for _, status := range []PlanStatus{
		PlanStatusPaused,
		PlanStatusAbandoned,
		PlanStatusArchived,
	} {
		t.Run(string(status), func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			planID, sessionID, _ := mustCreateActiveV2Task(t, s, "late-"+string(status))
			if err := s.SetPlanStatus(ctx, planID, status); err != nil {
				t.Fatalf("SetPlanStatus(%s): %v", status, err)
			}
			if _, err := s.MarkDone(ctx, planID, "task", sessionID, `{}`); err != nil {
				t.Fatalf("MarkDone: %v", err)
			}
			plan, err := s.GetPlan(ctx, planID)
			if err != nil {
				t.Fatalf("GetPlan: %v", err)
			}
			if plan.Status != status {
				t.Fatalf("late completion changed plan status to %s, want %s", plan.Status, status)
			}
		})
	}
}

func TestActivatingV2PlanRecomputesTerminalConvergence(t *testing.T) {
	t.Run("successful", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		planID, sessionID, _ := mustCreateActiveV2Task(t, s, "resume-success")
		if err := s.SetPlanStatus(ctx, planID, PlanStatusPaused); err != nil {
			t.Fatalf("pause: %v", err)
		}
		if _, err := s.MarkDone(ctx, planID, "task", sessionID, `{}`); err != nil {
			t.Fatalf("MarkDone: %v", err)
		}
		if err := s.SetPlanStatus(ctx, planID, PlanStatusActive); err != nil {
			t.Fatalf("resume: %v", err)
		}
		plan, err := s.GetPlan(ctx, planID)
		if err != nil {
			t.Fatalf("GetPlan: %v", err)
		}
		if plan.Status != PlanStatusDone {
			t.Fatalf("resumed successful plan status = %s, want %s", plan.Status, PlanStatusDone)
		}
	})

	t.Run("failed", func(t *testing.T) {
		s := openTestStore(t)
		ctx := context.Background()
		planID, sessionID, _ := mustCreateActiveV2Task(t, s, "resume-failed")
		if err := s.SetPlanStatus(ctx, planID, PlanStatusPaused); err != nil {
			t.Fatalf("pause: %v", err)
		}
		if _, err := s.MarkFailed(ctx, planID, "task", sessionID, "terminal", 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
		plan, err := s.GetPlan(ctx, planID)
		if err != nil {
			t.Fatalf("GetPlan paused: %v", err)
		}
		if plan.Status != PlanStatusPaused {
			t.Fatalf("late failure changed paused plan to %s", plan.Status)
		}
		if err := s.SetPlanStatus(ctx, planID, PlanStatusActive); err != nil {
			t.Fatalf("resume: %v", err)
		}
		plan, err = s.GetPlan(ctx, planID)
		if err != nil {
			t.Fatalf("GetPlan resumed: %v", err)
		}
		if plan.Status != PlanStatusFailedPartial {
			t.Fatalf("resumed failed plan status = %s, want %s", plan.Status, PlanStatusFailedPartial)
		}
	})
}
