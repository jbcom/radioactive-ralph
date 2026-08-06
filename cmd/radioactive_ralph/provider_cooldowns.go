package main

import (
	"sync"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// ProviderCooldowns tracks per-provider rate-limit/credit-exhaustion cooldowns
// so the pool rotation can skip providers whose limits have reset but not yet
// been confirmed. When a provider fails with provider_auth or
// provider_rejected due to usage limits, it enters a cooldown with
// exponential backoff. When the cooldown expires, the next dispatch to it IS
// the probe — a success clears the cooldown, another failure extends it.
//
// This is the mechanism that lets a multi-provider pool degrade gracefully:
// when claude credits run out, claude drops out of rotation and
// opencode/codex absorb the work. When claude's weekly limit resets, the
// cooldown expires and claude re-enters rotation automatically — no manual
// config change needed.
//
// Thread-safe: the binding resolver and the orchestrator's failure path
// touch this from different goroutines.
type ProviderCooldowns struct {
	mu      sync.Mutex
	entries map[string]cooldownEntry
	now     func() time.Time
}

type cooldownEntry struct {
	// expiry is when the cooldown ends and the provider becomes eligible
	// for probing again.
	expiry time.Time
	// consecutiveFailures drives the exponential backoff. Reset to 0 on
	// a successful probe.
	consecutiveFailures int
}

// cooldownDurations is the backoff schedule. Each entry is the cooldown
// duration after the Nth consecutive failure (1-indexed). After the last
// entry, the final duration is reused indefinitely.
var cooldownDurations = []time.Duration{
	1 * time.Hour,
	2 * time.Hour,
	4 * time.Hour,
	8 * time.Hour,
	24 * time.Hour,
}

// NewProviderCooldowns creates an empty cooldown tracker. The now function
// defaults to time.Now and can be overridden in tests.
func NewProviderCooldowns(now func() time.Time) *ProviderCooldowns {
	if now == nil {
		now = time.Now
	}
	return &ProviderCooldowns{
		entries: map[string]cooldownEntry{},
		now:     now,
	}
}

// RecordFailure extends a provider's cooldown after a failure that indicates
// rate-limiting or credit exhaustion. The failure category determines
// whether a cooldown is appropriate:
//   - provider_auth: the credential was rejected — a weekly/usage limit
//     resets on a known schedule, so cooldown + probe is the right strategy.
//   - provider_rejected: the provider reported an unsuccessful turn. This
//     includes quota/usage-limit messages that arrive as text rather than
//     HTTP status codes.
//
// Other failure categories (stall_timeout, turn_deadline, provider_unavailable,
// provider_throttled) are transient and handled by the retry budget — they
// do NOT enter cooldown because the provider is still functional, just
// temporarily unable to serve this turn.
func (c *ProviderCooldowns) RecordFailure(providerName string, failure provider.Failure) {
	if !shouldCooldown(failure.Category) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.entries[providerName]
	entry.consecutiveFailures++

	idx := entry.consecutiveFailures - 1
	if idx >= len(cooldownDurations) {
		idx = len(cooldownDurations) - 1
	}
	entry.expiry = c.now().Add(cooldownDurations[idx])
	c.entries[providerName] = entry
}

// RecordSuccess clears a provider's cooldown after a successful turn. A
// successful probe confirms the provider's limits have reset.
func (c *ProviderCooldowns) RecordSuccess(providerName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, providerName)
}

// Active returns the providers currently in cooldown (expiry not yet
// passed), along with when each one's cooldown ends. Used by the binding
// resolver to skip cooled providers in pool rotation.
func (c *ProviderCooldowns) Active() map[string]time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	active := map[string]time.Time{}
	for name, entry := range c.entries {
		if now.Before(entry.expiry) {
			active[name] = entry.expiry
		}
	}
	return active
}

// EarliestExpiry returns the provider with the soonest-expiring cooldown,
// or "" if no providers are in cooldown. Used when ALL providers are cooled
// — the resolver picks the earliest one so at least one dispatch can
// proceed after its cooldown ends.
func (c *ProviderCooldowns) EarliestExpiry() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	var earliest string
	var earliestTime time.Time
	for name, entry := range c.entries {
		if now.Before(entry.expiry) {
			if earliest == "" || entry.expiry.Before(earliestTime) {
				earliest = name
				earliestTime = entry.expiry
			}
		}
	}
	return earliest
}

// shouldCooldown reports whether a failure category warrants putting the
// provider in cooldown. Only auth and rejected failures enter cooldown,
// because those are the categories that rate-limit/credit-exhaustion
// messages map to (HTTP 401/403 for auth, text-based usage-limit messages
// for rejected). Throttling (429) and unavailable (5xx) are handled by the
// retry budget and do NOT enter cooldown — the provider is still
// functional, just temporarily rate-limited.
func shouldCooldown(category provider.FailureCategory) bool {
	switch category {
	case provider.FailureProviderAuth,
		provider.FailureProviderRejected:
		return true
	default:
		return false
	}
}
