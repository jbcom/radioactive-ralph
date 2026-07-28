package e2e

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agentdetect"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// TestE2E_LiveContainedTurnCompletes is the evidence the default-on flip asked
// for by name, and the ONE question the CI containment E2E cannot answer.
//
// TestE2E_ContainedTurnCannotWriteOutsideTheProject already proves the boundary
// HOLDS: a fake CLI told to write outside the project is stopped. But a fake CLI
// only attempts what the test scripts, so it cannot tell us whether a REAL agent
// CLI does something legitimate that the project-root policy forbids. That is
// the actual risk in turning containment on by default -- not that the boundary
// leaks, but that it breaks working setups on upgrade, since real CLIs touch
// caches, config, and temp dirs far outside any checkout.
//
// So this runs a real installed CLI, on a real turn, with contain_provider_writes
// set exactly as an operator sets it, and requires the task to reach `done`. A
// pass is a real turn surviving containment; a failure is the upgrade-breakage
// worry made concrete, with the events logged to show what the policy denied.
//
// Gated behind RALPH_E2E_LIVE=1 for the same reason its sibling is: it spends
// real money against a real hosted model, so it must never run in CI or by
// accident.
func TestE2E_LiveContainedTurnCompletes(t *testing.T) {
	if os.Getenv("RALPH_E2E_LIVE") != "1" {
		t.Skip("RALPH_E2E_LIVE != 1; skipping local-only live contained dispatch (set RALPH_E2E_LIVE=1 to run against a real installed CLI)")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("kernel-enforced containment is implemented for darwin and linux, not %s", runtime.GOOS)
	}

	detected := agentdetect.Detect()
	suggested := agentdetect.Suggest(detected)
	if len(suggested) == 0 {
		t.Skip("no supported agent CLI (claude/codex/opencode) detected on PATH; skipping live contained dispatch")
	}
	providerName := suggested[0]
	t.Logf("live contained dispatch: using detected provider %q", providerName)

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
	projectID, err := st.CreateProject(ctx, "e2e-live-contained", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// The operator's action, in the same key and encoding a real config write
	// uses. This is the whole point: the turn below runs under containment
	// because a config value said so, not because a test flag did.
	if err := st.SetProjectConfig(ctx, projectID, vconfig.ContainProviderWritesKey, `"true"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	planMarkdown := "# Live contained smoke\n\n1. Reply with a short confirmation that you received this task.\n"
	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "live-contained-plan",
		Title:          "Live contained smoke",
		SourceMarkdown: planMarkdown,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	o := orch.New(st,
		orch.WithBindingResolver(func(_ context.Context, _ string, _ bool, _ orch.BindingResolutionPurpose) (provider.Binding, error) {
			return provider.ResolveBinding(provider.File{}, provider.Local{}, provider.VariantFile{Provider: providerName})
		}),
		orch.WithSpendCap(providerName, liveSpendCapUSD),
		// The production wire, not a test shortcut: the same resolver
		// supervisor_cmd.go installs. A test-only bool here would prove the
		// sandbox works while saying nothing about whether an operator's config
		// reaches it -- the exact gap that let an inert config key ship once.
		orch.WithContainmentResolver(func(ctx context.Context, pid string) bool {
			userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
			if err != nil {
				return false
			}
			projectCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, pid)
			if err != nil {
				return false
			}
			return vconfig.ContainProviderWrites(projectCfg)
		}),
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
	o.Wait()

	task, err := st.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	t.Logf("live contained dispatch: task status = %s (retry_count=%d)", task.Status, task.RetryCount)
	if events, evErr := st.ListTaskEvents(ctx, planID, "0.0", 20); evErr == nil {
		for _, ev := range events {
			t.Logf("live contained dispatch: event kind=%s payload=%s", ev.Kind, ev.PayloadJSON)
		}
	}

	if task.Status != store.TaskStatusDone {
		t.Fatalf("a REAL provider turn did NOT complete under containment: status=%q "+
			"(retry_count=%d). This is the upgrade-breakage risk that gates defaulting "+
			"contain_provider_writes on -- a real CLI needs something the project-root "+
			"policy denies. See the logged events for what it attempted; do NOT flip the "+
			"default until this passes.", task.Status, task.RetryCount)
	}
}
