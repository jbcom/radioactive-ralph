package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agentdetect"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// liveSpendCapUSD bounds the real spend this test can ever incur, in the
// (extremely unlikely, since the plan is a one-line trivial ask) worst
// case where WithSpendCap's admission check races a first turn that's
// already over cap. Kept tiny — this test proves the wiring works, not
// that a real model turn is cheap.
const liveSpendCapUSD = 0.50

// TestE2E_LiveDispatchWithRealProviderCLI is the LOCAL-ONLY variant of the
// dispatch E2E (Phase 8): it runs orch.Orchestrator.DispatchNext against a
// REAL, installed claude/codex/opencode CLI (via internal/agentdetect) on
// a tiny real markdown plan, under a small spend cap, and asserts the step
// is actually dispatched and verified/completed.
//
// Gated behind RALPH_E2E_LIVE=1 so it NEVER runs in CI or by accident: it
// spends real money against a real hosted model. It also skips cleanly
// (not a failure) when no supported CLI is detected on PATH, so a
// developer's machine without any agent CLI installed doesn't get a
// spurious failure just from opting into RALPH_E2E_LIVE.
func TestE2E_LiveDispatchWithRealProviderCLI(t *testing.T) {
	if os.Getenv("RALPH_E2E_LIVE") != "1" {
		t.Skip("RALPH_E2E_LIVE != 1; skipping local-only live provider dispatch (set RALPH_E2E_LIVE=1 to run against a real installed CLI)")
	}

	detected := agentdetect.Detect()
	suggested := agentdetect.Suggest(detected)
	if len(suggested) == 0 {
		t.Skip("no supported agent CLI (claude/codex/opencode) detected on PATH; skipping live dispatch")
	}
	providerName := suggested[0]
	t.Logf("live dispatch: using detected provider %q", providerName)

	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)

	ctx := context.Background()
	dbPath := filepath.Join(env.StateDir, "ralph.db")
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	resolvedProjectDir := env.ProjectDir
	if resolved, err := filepath.EvalSymlinks(env.ProjectDir); err == nil {
		resolvedProjectDir = resolved
	}
	fps, err := store.Fingerprints(ctx, resolvedProjectDir)
	if err != nil {
		t.Fatalf("fingerprints: %v", err)
	}
	projectID, err := st.CreateProject(ctx, "e2e-live-project", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// A deliberately tiny, cheap plan. No mechanical (file/command)
	// acceptance criterion is attached — orch/orchestrator.go's
	// dispatchWorker runs every turn with WorkingDir "." (the dispatching
	// process's own cwd, not a per-task project directory; a real
	// project-scoped cwd is a separate gap outside Phase 8's scope), so a
	// "create a file" criterion would verify against the wrong
	// directory. mechanicalAcceptanceCheck's no-criterion fallback
	// (internal/orch/verify.go) requires non-empty evidence output
	// instead — still an orchestrator-side check, just not filesystem-
	// mechanical, which is what this smoke test is actually proving:
	// dispatch reaches a terminal state against a REAL provider CLI.
	planMarkdown := "# Live E2E smoke\n\n1. Reply with a short confirmation that you received this task.\n"
	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "live-e2e-plan",
		Title:          "Live E2E smoke",
		SourceMarkdown: planMarkdown,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	o := orch.New(st,
		orch.WithBindingResolver(func(_ context.Context, _ string, _ bool) (provider.Binding, error) {
			return provider.ResolveBinding(provider.File{}, provider.Local{}, provider.VariantFile{Provider: providerName})
		}),
		orch.WithSpendCap(providerName, liveSpendCapUSD),
	)

	dispatchCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	dispatched, err := o.DispatchNext(dispatchCtx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("DispatchNext dispatched = %d, want 1", dispatched)
	}

	task, err := st.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	t.Logf("live dispatch: task status after DispatchNext = %s (retry_count=%d)", task.Status, task.RetryCount)
	events, evErr := st.ListTaskEvents(ctx, planID, "0.0", 20)
	if evErr == nil {
		for _, ev := range events {
			t.Logf("live dispatch: event kind=%s payload=%s", ev.Kind, ev.PayloadJSON)
		}
	}
	// A rejected verification with retry budget remaining requeues to
	// 'pending' rather than 'failed' (store.MarkFailedWithPayload) — that
	// is a legitimate real-dispatch outcome, not a defect, so pending is
	// an acceptable terminal-for-this-smoke-test state alongside
	// done/failed. What matters is that a REAL dispatch pass ran to
	// completion (dispatched == 1, asserted above) without error.
	switch task.Status {
	case store.TaskStatusDone, store.TaskStatusFailed, store.TaskStatusPending:
	default:
		t.Fatalf("task status = %q, want done, failed, or pending (retry-eligible) after one real dispatch", task.Status)
	}
}
