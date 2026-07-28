package orch

import (
	"context"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// TestSaturationIsReportedNotSilent closes #249.
//
// Every other admission gate reports itself: a spend cap emits
// worker.admission_refused, and a capability or input block records a durable
// status. Dispatch-slot saturation reported NOTHING — both paths simply return
// or break when acquireDispatchSlot fails, and DispatchNext returns 0, nil.
//
// Zero dispatched with no event is byte-identical to an empty ready set, and
// those two states are the pair that most needs distinguishing: "waiting on
// dependencies" resolves itself as tasks complete, while "ready work, no
// capacity" may not — a wedged worker never frees its slot. An operator
// watching a plan sit at zero cannot currently tell which they are looking at.
func TestSaturationIsReportedNotSilent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "saturation")
	planID := mustCreateTestPlan(t, s, projectID, "saturation", "Sat", threeStepParallelPlan)

	// maxParallel=1 with a turn that blocks: the first step takes the only slot
	// and holds it, so the remaining ready steps hit saturation.
	release := make(chan struct{})
	runner := &slotHoldingRunner{release: release}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithMaxParallel(1),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 (one slot, so exactly one turn)", dispatched)
	}

	// A second pass has ready work and no capacity — the state under test.
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("second DispatchNext: %v", err)
	}

	events, err := s.ListProjectEvents(ctx, projectID, 200)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Kind == "dispatch.saturated" {
			found = true
			// The event has to say ready work is WAITING, not merely that a slot
			// was unavailable — otherwise it is as uninformative as silence.
			if !strings.Contains(e.PayloadJSON, "candidate") &&
				!strings.Contains(e.PayloadJSON, "waiting") {
				t.Errorf("payload = %s; it must convey that ready work is waiting "+
					"for capacity, not just that a slot was taken", e.PayloadJSON)
			}
		}
	}
	if !found {
		t.Fatal("no dispatch.saturated event: zero dispatched with no event is " +
			"indistinguishable from an empty ready set, which is exactly the " +
			"distinction an operator needs")
	}

	close(release)
	o.Wait()
}

// TestSaturationEmitsOnlyOnTransition applies the lesson #247 established: a
// condition re-evaluated every supervisor tick must emit when it is ENTERED,
// not once per tick, or the signal drowns in repeats of itself.
func TestSaturationEmitsOnlyOnTransition(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "saturation-once")
	planID := mustCreateTestPlan(t, s, projectID, "saturation-once", "Sat", threeStepParallelPlan)

	release := make(chan struct{})
	runner := &slotHoldingRunner{release: release}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithMaxParallel(1),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("seed DispatchNext: %v", err)
	}
	for pass := 0; pass < 4; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	events, err := s.ListProjectEvents(ctx, projectID, 400)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == "dispatch.saturated" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d dispatch.saturated events across 4 saturated passes, want 1 "+
			"— an unchanged condition must not re-emit per tick", n)
	}

	close(release)
	o.Wait()
}

// slotHoldingRunner holds its dispatch slot until released, so later steps in
// the same pass genuinely hit saturation rather than racing it.
type slotHoldingRunner struct{ release chan struct{} }

func (r *slotHoldingRunner) Run(ctx context.Context, _ provider.Binding, _ provider.Request) (provider.Result, error) {
	select {
	case <-r.release:
	case <-ctx.Done():
		return provider.Result{}, ctx.Err()
	}
	return provider.Result{AssistantOutput: "done"}, nil
}
