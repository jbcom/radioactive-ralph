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
	parent, cancelParent := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancelParent()
	ctx, progress, stop := withProgressLease(parent, 35*time.Millisecond)
	defer stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			progress()
		case <-ctx.Done():
			if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
				t.Fatalf("cause = %v, want absolute turn deadline", context.Cause(ctx))
			}
			return
		case <-time.After(time.Second):
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
