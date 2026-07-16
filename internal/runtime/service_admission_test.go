package runtime

import (
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// TestDurableAdmissionRefusesGatedVariant proves the durable service
// refuses to spawn a confirmation-gated destructive variant unless the
// operator has authorized it in local.toml — the fix for the durable-path
// gate bypass. It also confirms the attached path is unaffected (gates are
// enforced at the CLI there).
func TestDurableAdmissionRefusesGatedVariant(t *testing.T) {
	gated := mustLookupVariant(t, variant.WorldBreaker)
	if !gated.HasGate() {
		t.Fatalf("precondition: %s must be gated", gated.Name)
	}

	t.Run("durable refuses unconfirmed gated variant", func(t *testing.T) {
		s := &Service{opts: Options{SessionMode: plandag.SessionModeDurable}}
		reason, refused := s.durableAdmissionRefusal(gated)
		if !refused {
			t.Fatalf("expected refusal for unconfirmed %s, got admitted", gated.Name)
		}
		if reason == "" {
			t.Error("refusal reason must be non-empty for operator visibility")
		}
	})

	t.Run("durable admits confirmed gated variant with cap", func(t *testing.T) {
		spendCap := 25.0
		s := &Service{
			opts: Options{SessionMode: plandag.SessionModeDurable},
			local: config.Local{
				ConfirmDurableVariants: []string{string(gated.Name)},
			},
			cfg: config.File{
				Variants: map[string]config.VariantFile{
					string(gated.Name): {SpendCapUSD: &spendCap},
				},
			},
		}
		if _, refused := s.durableAdmissionRefusal(gated); refused {
			t.Errorf("expected admission for confirmed+capped %s, got refusal", gated.Name)
		}
	})

	t.Run("durable refuses confirmed gated variant without spend cap", func(t *testing.T) {
		s := &Service{
			opts: Options{SessionMode: plandag.SessionModeDurable},
			local: config.Local{
				ConfirmDurableVariants: []string{string(gated.Name)},
			},
		}
		reason, refused := s.durableAdmissionRefusal(gated)
		if !refused {
			t.Fatalf("expected refusal for confirmed-but-uncapped %s", gated.Name)
		}
		if reason == "" {
			t.Error("refusal reason must be non-empty")
		}
	})

	t.Run("attached mode never refuses here", func(t *testing.T) {
		s := &Service{opts: Options{SessionMode: plandag.SessionModeAttached}}
		if _, refused := s.durableAdmissionRefusal(gated); refused {
			t.Error("attached mode gates at the CLI, not in durableAdmissionRefusal")
		}
	})
}

// TestDurableAdmissionAllowsUngatedVariant confirms ordinary variants
// (no gate, no spend-cap floor) dispatch without operator authorization.
func TestDurableAdmissionAllowsUngatedVariant(t *testing.T) {
	p := mustLookupVariant(t, variant.Green)
	if p.HasGate() || p.SafetyFloors.RequireSpendCap {
		t.Skipf("green unexpectedly gated/capped; picked wrong fixture")
	}
	s := &Service{opts: Options{SessionMode: plandag.SessionModeDurable}}
	if reason, refused := s.durableAdmissionRefusal(p); refused {
		t.Errorf("ungated variant %s refused: %s", p.Name, reason)
	}
}

// TestSpendCapEnforced proves a capped variant is admitted until its
// accumulated provider cost reaches the cap, then refused — the fix for
// the spend cap being validated-for-presence but never enforced.
func TestSpendCapEnforced(t *testing.T) {
	gated := mustLookupVariant(t, variant.WorldBreaker)
	if !gated.SafetyFloors.RequireSpendCap {
		t.Skipf("%s unexpectedly has no spend-cap floor", gated.Name)
	}
	capUSD := 1.00
	s := &Service{
		opts:           Options{SessionMode: plandag.SessionModeDurable},
		spendByVariant: map[variant.Name]float64{},
		local: config.Local{
			ConfirmDurableVariants: []string{string(gated.Name)},
		},
		cfg: config.File{
			Variants: map[string]config.VariantFile{
				string(gated.Name): {SpendCapUSD: &capUSD},
			},
		},
	}

	// Under the cap: admitted.
	if _, refused := s.durableAdmissionRefusal(gated); refused {
		t.Fatalf("expected admission below cap")
	}

	// Burn $0.60, still under $1.00: admitted.
	s.recordSpend(t.Context(), plandag.Plan{ID: "p"}, plandag.Task{ID: "t"}, gated, "claude", provider.Usage{CostUSD: 0.60})
	if _, refused := s.durableAdmissionRefusal(gated); refused {
		t.Fatalf("expected admission at $0.60 of $1.00")
	}

	// Cross the cap: refused.
	s.recordSpend(t.Context(), plandag.Plan{ID: "p"}, plandag.Task{ID: "t"}, gated, "claude", provider.Usage{CostUSD: 0.50})
	reason, refused := s.durableAdmissionRefusal(gated)
	if !refused {
		t.Fatalf("expected refusal after crossing cap; total=%.2f", s.spentForVariant(gated.Name))
	}
	if reason == "" {
		t.Error("refusal reason must be non-empty")
	}
}

func mustLookupVariant(t *testing.T, name variant.Name) variant.Profile {
	t.Helper()
	p, err := variant.Lookup(string(name))
	if err != nil {
		t.Fatalf("variant.Lookup(%q): %v", name, err)
	}
	return p
}
