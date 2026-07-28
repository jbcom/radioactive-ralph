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

// TestE2E_ContainedTurnCannotWriteOutsideTheProject is the REAL-TURN evidence
// that turning containment on by default requires.
//
// Everything before this proved containment one layer at a time: the policy
// wraps a command, the runners pass a root, the config key resolves. None of
// them ran the actual pipeline. This does — real supervisor process, real
// orchestrator, real provider subprocess under its own pty — with the project
// config key set exactly as an operator would set it, and a provider that
// genuinely attempts to write outside the project.
//
// It is the layer where the two earlier failures would have been caught. The
// stream-json shape that skipped applyContainment and the config key nothing
// consulted BOTH passed every unit test around them, because each layer was
// asked "did you do your part?" rather than "did the write escape?".
func TestE2E_ContainedTurnCannotWriteOutsideTheProject(t *testing.T) {
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
	projectID, err := st.CreateProject(context.Background(), "e2e-contained", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// The operator's action, through the same key and encoding a real config
	// write uses. If this key stopped reaching dispatch, the escape below would
	// succeed and this test would fail — which is the regression that shipped
	// once already.
	if err := st.SetProjectConfig(
		context.Background(), projectID, vconfig.ContainProviderWritesKey, `"true"`,
	); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	planTitle := "Contained turn"
	planID, err := st.CreatePlan(context.Background(), store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "e2e-contained-plan",
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

// TestE2E_UncontainedTurnCanWriteOutsideTheProject is the control, and without
// it the test above proves nothing.
//
// If the fake provider's write failed for an unrelated reason — a bad path, a
// quoting bug, a script that never ran — the contained case would pass while
// measuring nothing at all. This asserts the same provider, same plan, same
// pipeline DOES escape when the operator has not asked for containment, which
// is what makes the contained result attributable to containment.
func TestE2E_UncontainedTurnCanWriteOutsideTheProject(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("paired with the contained case, which is darwin/linux only")
	}

	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)

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
	projectID, err := st.CreateProject(context.Background(), "e2e-uncontained", fps)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// No config key set: containment off, exactly as an untouched project is.

	planTitle := "Uncontained turn"
	planID, err := st.CreatePlan(context.Background(), store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "e2e-uncontained-plan",
		Title:          planTitle,
		SourceMarkdown: "# " + planTitle + "\n\n1. write outside the project\n",
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
	)

	dispatchCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := o.DispatchNext(dispatchCtx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if _, err := os.Stat(escape); err != nil {
		t.Fatalf("the fake provider did NOT write outside the project even with "+
			"containment off (%v) — so the contained case above cannot distinguish "+
			"containment from a write that never happened", err)
	}
}
