package runtime

import (
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/db"
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

// TestSpendCapReservesInFlight proves a capped variant with a turn already
// in flight is refused, so parallelism cannot overshoot a small cap before
// any cost is recorded.
func TestSpendCapReservesInFlight(t *testing.T) {
	gated := mustLookupVariant(t, variant.WorldBreaker)
	capUSD := 100.00 // high cap so only the in-flight rule can refuse
	s := &Service{
		opts:           Options{SessionMode: plandag.SessionModeDurable},
		spendByVariant: map[variant.Name]float64{},
		workers:        map[string]workerState{},
		local:          config.Local{ConfirmDurableVariants: []string{string(gated.Name)}},
		cfg: config.File{
			Variants: map[string]config.VariantFile{
				string(gated.Name): {SpendCapUSD: &capUSD},
			},
		},
	}

	// No worker in flight: admitted.
	if _, refused := s.durableAdmissionRefusal(gated); refused {
		t.Fatalf("expected admission with no in-flight worker")
	}

	// One turn in flight: a second is refused until its cost lands.
	s.workers["p:t"] = workerState{Variant: gated.Name}
	reason, refused := s.durableAdmissionRefusal(gated)
	if !refused {
		t.Fatalf("expected refusal while a capped turn is in flight")
	}
	if reason == "" {
		t.Error("refusal reason must be non-empty")
	}
}

// TestRefusalEventsDeduped proves the scheduler logs an admission refusal
// only when the reason first appears or changes, not on every tick.
func TestRefusalEventsDeduped(t *testing.T) {
	gated := mustLookupVariant(t, variant.WorldBreaker)
	s := &Service{
		opts:        Options{SessionMode: plandag.SessionModeDurable},
		lastRefusal: map[string]string{},
		// eventDB nil: logEvent is a no-op, but lastRefusal dedup state
		// still updates, which is what we assert.
	}
	plan := plandag.Plan{ID: "p"}
	task := plandag.Task{ID: "t"}

	s.logAdmissionRefusal(t.Context(), plan, task, gated, "reason A")
	if got := s.lastRefusal["p:t"]; got != "reason A" {
		t.Fatalf("lastRefusal = %q, want reason A", got)
	}
	// Same reason again: no state change (would-be duplicate suppressed).
	s.logAdmissionRefusal(t.Context(), plan, task, gated, "reason A")
	if got := s.lastRefusal["p:t"]; got != "reason A" {
		t.Fatalf("lastRefusal changed on duplicate: %q", got)
	}
	// New reason: recorded.
	s.logAdmissionRefusal(t.Context(), plan, task, gated, "reason B")
	if got := s.lastRefusal["p:t"]; got != "reason B" {
		t.Fatalf("lastRefusal = %q, want reason B", got)
	}
}

// TestRestoreSpendFromEventLog proves the spend cap survives a restart:
// prior worker.spend events are summed back into spendByVariant, so a
// capped variant that already burned its cap stays refused after a restart
// instead of resetting to zero.
func TestRestoreSpendFromEventLog(t *testing.T) {
	gated := mustLookupVariant(t, variant.WorldBreaker)
	dbPath := filepath.Join(t.TempDir(), "state.db")
	eventDB, err := db.Open(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	// Simulate a prior service instance recording spend, then "restart"
	// by opening a fresh Service against the same event DB.
	prior := &Service{eventDB: eventDB, spendByVariant: map[variant.Name]float64{}}
	prior.recordSpend(t.Context(), plandag.Plan{ID: "p"}, plandag.Task{ID: "t1"}, gated, "claude", provider.Usage{CostUSD: 0.70})
	prior.recordSpend(t.Context(), plandag.Plan{ID: "p"}, plandag.Task{ID: "t2"}, gated, "claude", provider.Usage{CostUSD: 0.40})

	restarted := &Service{eventDB: eventDB, spendByVariant: map[variant.Name]float64{}}
	if err := restarted.restoreSpend(t.Context()); err != nil {
		t.Fatalf("restoreSpend: %v", err)
	}
	if got := restarted.spentForVariant(gated.Name); got < 1.09 || got > 1.11 {
		t.Fatalf("restored spend = %.4f, want ~1.10", got)
	}
	_ = eventDB.Close()
}

func mustLookupVariant(t *testing.T, name variant.Name) variant.Profile {
	t.Helper()
	p, err := variant.Lookup(string(name))
	if err != nil {
		t.Fatalf("variant.Lookup(%q): %v", name, err)
	}
	return p
}
