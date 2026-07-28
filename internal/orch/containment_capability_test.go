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
	var ran bool
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
			return recordingRunner{onRun: func(req provider.Request) {
				ran = true
				seenRoot = req.ContainmentRoot
			}}, nil
		}),
		// Containment is NOT requested for this project. That is the case this
		// test is about: an incapable provider runs normally when nobody asked
		// for a boundary. (When containment IS requested, the turn is REFUSED
		// instead -- see TestIncapableBindingIsRefusedWhenContainmentRequested.
		// Asserting the unconfined-root case with containment requested would
		// pass VACUOUSLY, because a refused turn never runs and so never
		// records a root.)
		WithContainmentResolver(func(context.Context, string) bool { return false }),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if !ran {
		t.Fatal("the turn never ran: an incapable provider must still work normally " +
			"when containment was not requested, or this test passes vacuously by " +
			"refusing rather than by sparing")
	}
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

// TestIncapableBindingIsRefusedWhenContainmentRequested is the correction to
// the first version of this feature, which was a SILENT SECURITY DOWNGRADE.
//
// Sparing an incapable provider is right when containment is not requested. But
// when a project explicitly sets contain_provider_writes, silently running that
// turn UNCONFINED gives the operator the opposite of what they asked for, with
// no error and no event -- and in a mixed pool, turns would alternate between
// confined and unconfined with nothing distinguishing them.
//
// That is worse than the opaque failure it replaced: an unexplained exit at
// least stops. A silent downgrade proceeds with full write access while the
// config claims a boundary, which is the "config that lies" failure this
// codebase already refuses elsewhere.
//
// So a REQUESTED containment that cannot be honoured refuses the dispatch and
// says why. The turn does not run rather than running unprotected.
func TestIncapableBindingIsRefusedWhenContainmentRequested(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contain-refuse")
	planID := mustCreateTestPlan(t, s, projectID, "contain-refuse", "Work",
		"# Work\n\n- do it\n\n   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n")

	no := false
	var ran bool
	o := New(s,
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name: "codex",
				Config: provider.BindingConfig{
					Type: "codex", Binary: "true", SupportsContainment: &no,
				},
			}, nil
		}),
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return recordingRunner{onRun: func(provider.Request) { ran = true }}, nil
		}),
		// The operator ASKED for containment.
		WithContainmentResolver(func(context.Context, string) bool { return true }),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if ran {
		t.Fatal("a turn RAN on a binding that cannot be confined while the project " +
			"requested containment; it ran with full write access and no signal, so " +
			"the operator believes a boundary is active that never existed")
	}

	// And the refusal must be VISIBLE, not merely a non-dispatch.
	events, err := s.ListTaskEvents(ctx, planID, "a", 20)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	var refused bool
	for _, ev := range events {
		if ev.Kind == "worker.admission_refused" {
			refused = true
		}
	}
	if !refused {
		t.Fatal("no worker.admission_refused event: a turn that does not run must say " +
			"why, or the plan simply stalls with no operator-visible reason")
	}
}

// TestStaticFlagAlsoRefusesAnIncapableBinding covers the OTHER way containment
// is requested: WithProviderWriteContainment(true), not the project config.
//
// That option promises to confine every provider turn. Sparing an incapable
// binding under it would break the same security contract as the config path,
// and an existing caller passing the flag has no project key to consult -- so
// the refusal must come from the flag itself, not only from the resolver.
func TestStaticFlagAlsoRefusesAnIncapableBinding(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contain-static")
	planID := mustCreateTestPlan(t, s, projectID, "contain-static", "Work",
		"# Work\n\n- do it\n\n   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n")

	no := false
	var ran bool
	o := New(s,
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name: "codex",
				Config: provider.BindingConfig{
					Type: "codex", Binary: "true", SupportsContainment: &no,
				},
			}, nil
		}),
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return recordingRunner{onRun: func(provider.Request) { ran = true }}, nil
		}),
		// No resolver: the STATIC flag is the request.
		WithProviderWriteContainment(true),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if ran {
		t.Fatal("a turn ran unconfined under WithProviderWriteContainment(true) with " +
			"a binding that cannot be confined; the option promises to confine EVERY " +
			"turn, so silently running one unprotected breaks that contract")
	}
}
