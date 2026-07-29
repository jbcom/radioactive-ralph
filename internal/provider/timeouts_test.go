package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestResolveTurnLimitsPrecedenceAndIndependentBounds(t *testing.T) {
	binding := Binding{
		Name: "codex",
		Config: BindingConfig{
			TurnTimeout:  "45m",
			StallTimeout: "4m",
		},
	}
	got, err := ResolveTurnLimits(binding, Request{
		TurnTimeout:  90 * time.Minute,
		StallTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("ResolveTurnLimits: %v", err)
	}
	if got.TurnTimeout != 90*time.Minute || got.StallTimeout != 30*time.Second {
		t.Fatalf("limits = %+v, want independent request overrides", got)
	}
}

func TestResolveTurnLimitsDefaultsAreBoundedAndDistinct(t *testing.T) {
	got, err := ResolveTurnLimits(Binding{Name: "claude"}, Request{})
	if err != nil {
		t.Fatalf("ResolveTurnLimits: %v", err)
	}
	if got.TurnTimeout != DefaultTurnTimeout {
		t.Errorf("turn timeout = %s, want %s", got.TurnTimeout, DefaultTurnTimeout)
	}
	if got.StallTimeout != DefaultStallTimeout {
		t.Errorf("stall timeout = %s, want %s", got.StallTimeout, DefaultStallTimeout)
	}
	if got.TurnTimeout == got.StallTimeout {
		t.Fatal("turn deadline silently reused the stall timeout")
	}
}

func TestResolveTurnLimitsRejectsUnboundedOrExcessiveValues(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
		req     Request
	}{
		{name: "zero config turn", binding: Binding{Name: "x", Config: BindingConfig{TurnTimeout: "0s"}}},
		{name: "negative config stall", binding: Binding{Name: "x", Config: BindingConfig{StallTimeout: "-1s"}}},
		{name: "excessive config turn", binding: Binding{Name: "x", Config: BindingConfig{TurnTimeout: "25h"}}},
		{name: "excessive request stall", binding: Binding{Name: "x"}, req: Request{StallTimeout: 2 * time.Hour}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ResolveTurnLimits(tt.binding, tt.req); err == nil {
				t.Fatal("ResolveTurnLimits unexpectedly accepted unsafe value")
			}
		})
	}
}

func TestProgressLeaseRenewsStallButNeverTotalDeadline(t *testing.T) {
	// The invariant under test is a RELATIONSHIP -- progress renews the stall
	// lease but never extends the absolute deadline -- so the only thing the
	// timings must guarantee is that the renewal loop cannot lose a race it is
	// not supposed to be in.
	//
	// It flaked in CI (GUI (macos-latest), #269) reporting
	// "cause = provider: progress stalled, want absolute turn deadline". At a
	// 10ms tick renewing a 35ms lease the margin is 3.5 ticks: ONE scheduling
	// gap of 35ms on a loaded runner trips the stall lease and the test
	// reports a false failure of an invariant that never broke.
	//
	// Widened to a 20x margin. The absolute deadline grows with it, so the
	// assertion is unchanged in kind -- this buys scheduling slack, it does
	// not weaken what is being proven. Both bounds stay well inside the
	// 1-second backstop below.
	const tick = 10 * time.Millisecond
	const stallLease = 20 * tick // 200ms: 20 missed ticks before a false stall
	const totalDeadline = 3 * stallLease

	parent, cancelParent := context.WithTimeout(context.Background(), totalDeadline)
	defer cancelParent()
	ctx, progress, stop := withProgressLease(parent, stallLease)
	defer stop()

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	// A single backstop for the whole loop. `case <-time.After(...)` inside
	// the select is re-armed on EVERY tick, so it can never accumulate and
	// the "progress extended the absolute deadline" branch is unreachable --
	// a violation shows up as go test's 30s/10m panic instead of this
	// assertion. Verified: detaching the lease from its parent makes the old
	// form panic on timeout rather than report. Hoisted so it fires once.
	backstop := time.After(4 * totalDeadline)
	for {
		select {
		case <-ticker.C:
			progress()
		case <-ctx.Done():
			if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
				t.Fatalf("cause = %v, want absolute turn deadline", context.Cause(ctx))
			}
			return
		case <-backstop:
			t.Fatal("progress extended the absolute deadline")
		}
	}
}

func TestProgressLeaseReportsTypedStall(t *testing.T) {
	ctx, _, stop := withProgressLease(context.Background(), 20*time.Millisecond)
	defer stop()
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), ErrProviderStalled) {
			t.Fatalf("cause = %v, want ErrProviderStalled", context.Cause(ctx))
		}
	case <-time.After(time.Second):
		t.Fatal("stall lease did not fire")
	}
}
