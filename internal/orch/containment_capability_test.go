package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// TestIncapableBindingIsNotConfined pins that a provider declaring it cannot
// run confined is dispatched UNCONFINED rather than handed a containment root
// it will die on.
//
// Without this the capability is inert -- declared, documented, and consulted
// by nothing -- which is the exact defect class this codebase has now closed
// four times (the contain config key, WithProviderWriteContainment, the
// declarative stream-json shape, and ralph-task `binding`). A flag with no
// consumer reads as a guarantee while changing nothing.
//
// The operator-visible outcome matters as much as the mechanism: before this,
// a contained codex turn failed with a bare nonzero exit AND a retry, so
// nothing named containment as the cause and the retry could never succeed.
func TestIncapableBindingIsNotConfined(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contain-incapable")
	planID := mustCreateTestPlan(t, s, projectID, "contain-incapable", "Work",
		"# Work\n\n- do it\n\n   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n")

	no := false
	var seenRoot string
	o := New(s,
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name: "codex",
				Config: provider.BindingConfig{
					Type: "codex", Binary: "true",
					SupportsContainment: &no,
				},
			}, nil
		}),
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return recordingRunner{onRun: func(req provider.Request) { seenRoot = req.ContainmentRoot }}, nil
		}),
		// Containment is ON for this project: the capability, not the config,
		// is what must spare this binding.
		WithContainmentResolver(func(context.Context, string) bool { return true }),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if seenRoot != "" {
		t.Fatalf("a binding declaring SupportsContainment=false was handed "+
			"ContainmentRoot=%q; it cannot start confined, so the turn dies with a "+
			"bare nonzero exit that names nothing and retries pointlessly", seenRoot)
	}
}

// TestCapableBindingIsStillConfined is the other half. Without it the change is
// satisfied by never confining anything, which would silently disable the
// boundary everywhere instead of only where it cannot work.
func TestCapableBindingIsStillConfined(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contain-capable")
	planID := mustCreateTestPlan(t, s, projectID, "contain-capable", "Work",
		"# Work\n\n- do it\n\n   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n")

	var seenRoot string
	o := New(s,
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			// No SupportsContainment set: absent means capable.
			return provider.Binding{
				Name:   "claude",
				Config: provider.BindingConfig{Type: "claude", Binary: "true"},
			}, nil
		}),
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return recordingRunner{onRun: func(req provider.Request) { seenRoot = req.ContainmentRoot }}, nil
		}),
		WithContainmentResolver(func(context.Context, string) bool { return true }),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if seenRoot == "" {
		t.Fatal("a containment-CAPABLE binding ran unconfined while the project " +
			"asked for containment; sparing incapable providers must not become " +
			"sparing every provider")
	}
}

type recordingRunner struct{ onRun func(provider.Request) }

func (r recordingRunner) Run(_ context.Context, _ provider.Binding, req provider.Request) (provider.Result, error) {
	r.onRun(req)
	return provider.Result{AssistantOutput: "done"}, nil
}
