package vconfig

import "strconv"

// ContainProviderWritesKey is the project config key that confines provider
// writes to the project directory, enforced by the kernel (internal/contain).
const ContainProviderWritesKey = "contain_provider_writes"

// ContainProviderWrites reports whether an operator has asked for provider
// write containment on this project.
//
// Absent means ON, and so does a malformed value.
//
// It defaulted OFF while the guarantee was unproven: enabling containment
// changes what a provider may do, and one that legitimately writes outside the
// checkout would start failing on upgrade. That was not a guess worth making
// from the test suite alone — and the caution was justified, because the first
// attempt to flip this DID break codex and opencode.
//
// It is no longer a guess. A binding now declares whether it can run confined
// at all (BindingConfig.SupportsContainment), so a provider that cannot is
// spared rather than broken, and every shipped provider completes a real turn
// with containment enabled. Enforcement is proven separately by
// TestE2E_ContainedTurnCannotWriteOutsideTheProject, which fails when
// containment is disabled and runs in a required check.
//
// An explicit false always wins: the flip changes the default, not the
// operator's ability to override it.
func ContainProviderWrites(cfg ProjectConfig) bool {
	raw, ok := cfg.Values[ContainProviderWritesKey]
	if !ok {
		return true
	}
	switch val := raw.(type) {
	case bool:
		return val
	case string:
		// A store layer round-trips values as JSON strings, so the same intent
		// arrives as "true" rather than true depending on which layer set it.
		parsed, err := strconv.ParseBool(val)
		if err != nil {
			// A MALFORMED value keeps containment ON, and this direction
			// INVERTED with the default. While absent meant off, a typo
			// silently ENABLING the boundary would surface as unexplained
			// provider write failures far from their cause. Now that absent
			// means on, a typo must not silently REMOVE a boundary the operator
			// believes is active -- the failure nobody notices until it matters.
			// Both readings choose "behave like the absent key"; only the key's
			// meaning changed.
			return true
		}
		return parsed
	default:
		return true
	}
}
