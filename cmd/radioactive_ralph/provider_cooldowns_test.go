package main

import (
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

func TestProviderCooldownsRecordFailureSetsExpiry(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	cd.RecordFailure("claude", provider.Failure{
		Category: provider.FailureProviderAuth,
		Summary:  "weekly limit exceeded",
	})

	active := cd.Active()
	if _, ok := active["claude"]; !ok {
		t.Fatal("claude should be in cooldown after a provider_auth failure")
	}
	// First failure → 1 hour backoff
	if !active["claude"].Equal(now.Add(1 * time.Hour)) {
		t.Fatalf("cooldown expiry = %v, want %v (1 hour after first failure)", active["claude"], now.Add(1*time.Hour))
	}
}

func TestProviderCooldownsExponentialBackoff(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	// First failure: 1h
	cd.RecordFailure("claude", provider.Failure{Category: provider.FailureProviderAuth})
	active := cd.Active()
	if !active["claude"].Equal(now.Add(1 * time.Hour)) {
		t.Fatalf("1st failure: expiry = %v, want %v", active["claude"], now.Add(1*time.Hour))
	}

	// Second failure: 2h
	cd.RecordFailure("claude", provider.Failure{Category: provider.FailureProviderAuth})
	active = cd.Active()
	if !active["claude"].Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("2nd failure: expiry = %v, want %v", active["claude"], now.Add(2*time.Hour))
	}

	// Third failure: 4h
	cd.RecordFailure("claude", provider.Failure{Category: provider.FailureProviderRejected})
	active = cd.Active()
	if !active["claude"].Equal(now.Add(4 * time.Hour)) {
		t.Fatalf("3rd failure: expiry = %v, want %v", active["claude"], now.Add(4*time.Hour))
	}
}

func TestProviderCooldownsRecordSuccessClears(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	cd.RecordFailure("claude", provider.Failure{Category: provider.FailureProviderAuth})
	if len(cd.Active()) != 1 {
		t.Fatalf("after failure, Active() = %d entries, want 1", len(cd.Active()))
	}

	cd.RecordSuccess("claude")
	if len(cd.Active()) != 0 {
		t.Fatalf("after success, Active() = %d entries, want 0", len(cd.Active()))
	}
}

func TestProviderCooldownsIgnoresNonCooldownFailures(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	// Stall, unavailable, and runtime should NOT enter cooldown —
	// those are transient or unrelated and handled by the retry budget.
	// Throttling DOES enter cooldown now (see shouldCooldown).
	for _, cat := range []provider.FailureCategory{
		provider.FailureStall,
		provider.FailureProviderUnavailable,
		provider.FailureRuntime,
	} {
		cd.RecordFailure("claude", provider.Failure{Category: cat})
	}
	if len(cd.Active()) != 0 {
		t.Fatalf("transient failures should not enter cooldown, Active() = %d", len(cd.Active()))
	}
}

func TestProviderCooldownsExpiryClearsAfterTime(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	cd.RecordFailure("claude", provider.Failure{Category: provider.FailureProviderAuth})
	if len(cd.Active()) != 1 {
		t.Fatal("should be in cooldown immediately after failure")
	}

	// Advance time past the 1-hour cooldown
	now = now.Add(2 * time.Hour)
	if len(cd.Active()) != 0 {
		t.Fatalf("after cooldown expires, Active() = %d, want 0", len(cd.Active()))
	}
}

func TestProviderCooldownsEarliestExpiry(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	cd.RecordFailure("claude", provider.Failure{Category: provider.FailureProviderAuth})    // 1h
	cd.RecordFailure("codex", provider.Failure{Category: provider.FailureProviderRejected}) // 1h

	earliest := cd.EarliestExpiry()
	if earliest != "claude" && earliest != "codex" {
		t.Fatalf("EarliestExpiry() = %q, want claude or codex", earliest)
	}
}

func TestProviderCooldownsEarliestExpiryEmpty(t *testing.T) {
	now := time.Date(2026, 8, 6, 16, 0, 0, 0, time.UTC)
	cd := NewProviderCooldowns(func() time.Time { return now })

	if earliest := cd.EarliestExpiry(); earliest != "" {
		t.Fatalf("EarliestExpiry() = %q, want empty string when no cooldowns", earliest)
	}
}
