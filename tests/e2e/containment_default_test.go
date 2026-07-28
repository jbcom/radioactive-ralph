package e2e

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// TestE2E_ContainmentIsOnWithoutAnyConfig proves the flipped default REACHES
// dispatch, on a project that sets NO containment key at all.
//
// Asserting on vconfig.ContainProviderWrites alone would prove only that a
// boolean changed — the "each layer did its part" shape that let an inert
// config key, an uncalled option, and an unconfined runner shape each ship
// looking complete. This measures whether a write ESCAPES.

func TestE2E_ContainmentIsOnWithoutAnyConfig(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("kernel-enforced containment is implemented for darwin and linux, not %s", runtime.GOOS)
	}

	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)

	// OUTSIDE the project, and outside the state dir: the target has to be
	// somewhere a provider could plausibly reach and must not be confused with
	// the project root the policy permits.
	outside := t.TempDir()
	escape := filepath.Join(outside, "escaped")
	fakeDir := WriteEscapingFakeClaudeCLI(t, escape)

	dbPath := filepath.Join(env.StateDir, "ralph.db")
	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	resolvedProjectDir := env.ProjectDir
	if resolved, err := filepath.EvalSymlinks(env.ProjectDir); err == nil {
		resolvedProjectDir = resolved
	}
	fps, err := store.Fingerprints(context.Background(), resolvedProjectDir)
	if err != nil {
		t.Fatalf("fingerprints: %v", err)
	}
	projectID, err := st.CreateProject(context.Background(), "e2e-default-contained", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	planTitle := "Contained turn"
	planID, err := st.CreatePlan(context.Background(), store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "e2e-default-contained-plan",
		Title:          planTitle,
		SourceMarkdown: "# " + planTitle + "\n\n1. try to escape the project\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(context.Background(), planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	fakeClaudeBin := filepath.Join(fakeDir, "claude")
	o := orch.New(st,
		orch.WithBindingResolver(func(_ context.Context, _ string, _ bool, _ orch.BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name:   "claude",
				Config: provider.BindingConfig{Type: "claude", Binary: fakeClaudeBin},
			}, nil
		}),
		// The production wire: the same resolver supervisor_cmd.go installs, so
		// this exercises the config path rather than a test-only shortcut.
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

	dispatchCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := o.DispatchNext(dispatchCtx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if _, err := os.Stat(escape); err == nil {
		t.Fatal("a REAL provider turn wrote outside the project while " +
			"contain_provider_writes was set — the config key does not reach the " +
			"kernel boundary, so an operator who enabled containment has none")
	}
}
