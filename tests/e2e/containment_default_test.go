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

// TestE2E_ContainmentIsOnWithoutAnyConfig is the end-to-end proof that the
// flipped default REACHES dispatch.
//
// vconfig.ContainProviderWrites defaulting to true is necessary but not
// sufficient: this project sets NO containment key at all, and the turn must
// still be confined. Asserting on the vconfig function alone would have proved
// only that a boolean changed -- the exact "each layer did its part" shape that
// let an inert config key, an uncalled option, and an unconfined runner shape
// each ship looking complete.
//
// It measures whether a write ESCAPES, not whether a flag was passed.
func TestE2E_ContainmentIsOnWithoutAnyConfig(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("kernel-enforced containment is implemented for darwin and linux, not %s", runtime.GOOS)
	}

	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)

	outside := t.TempDir()
	escape := filepath.Join(outside, "escaped")
	fakeDir := WriteEscapingFakeClaudeCLI(t, escape)

	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(filepath.Join(env.StateDir, "ralph.db"))})
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
	projectID, err := st.CreateProject(ctx, "e2e-default-contained", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// DELIBERATELY no SetProjectConfig call. That absence IS the test.

	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "e2e-default-contained-plan",
		Title:          "Default contained",
		SourceMarkdown: "# Default contained\n\n1. try to escape the project\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	o := orch.New(st,
		orch.WithBindingResolver(func(_ context.Context, _ string, _ bool, _ orch.BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name:   "claude",
				Config: provider.BindingConfig{Type: "claude", Binary: filepath.Join(fakeDir, "claude")},
			}, nil
		}),
		// The production wire from supervisor_cmd.go, unchanged. With no key
		// stored, this resolver is what must answer "yes" on its own.
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

	dispatchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := o.DispatchNext(dispatchCtx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if _, err := os.Stat(escape); err == nil {
		t.Fatal("a provider turn wrote OUTSIDE the project on a config that never " +
			"mentioned containment -- the on-by-default decision does not reach " +
			"dispatch, so every deployment that has not opted in is unprotected " +
			"while the default claims otherwise")
	}
}
