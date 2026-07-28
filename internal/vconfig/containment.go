package vconfig

import "strconv"

// ContainProviderWritesKey is the project config key that confines provider
// writes to the project directory, enforced by the kernel (internal/contain).
const ContainProviderWritesKey = "contain_provider_writes"

// ContainProviderWrites reports whether an operator has asked for provider
// write containment on this project.
//
// Absent means OFF. Enabling containment changes what a provider is permitted
// to do — one that legitimately writes to a shared cache outside the checkout
// starts failing — so it is an operator's decision rather than something an
// upgrade turns on.
//
// A malformed value is also OFF. A typo must not silently ENABLE a boundary
// that makes provider writes fail, because that surfaces as unexplained
// provider errors far from their actual cause; behaving like the absent key it
// resembles is the honest failure direction.
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
			// A MALFORMED value keeps containment ON, and this inverted with the
			// default. While absent meant off, a typo silently enabling a
			// boundary would surface as unexplained provider errors far from
			// their cause, so resembling the absent key was the honest direction.
			// Now that absent means ON, the same argument points the other way:
			// a typo must not silently REMOVE a security boundary the operator
			// believes is active. Both readings pick "behave like the absent
			// key"; only the key's meaning changed.
			return true
		}
		return parsed
	default:
		return true
	}
}
