package provider

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Capability keys a plan task may name in its `requires` list.
//
// The vocabulary is CLOSED and every key maps to something BindingConfig actually
// declares. An open vocabulary would make `requires` unfalsifiable: a key no
// binding could ever satisfy would block every task forever, and a key no
// binding could ever fail would be decoration.
const (
	// CapabilityNativeFanout — the provider can run a whole ready group in one
	// turn. Dispatch charges such a group a single worker, so a task that needs
	// it must not land on a binding without it.
	CapabilityNativeFanout = "native_fanout"

	// CapabilityResume — the provider can continue a prior session. A task
	// written to pick up where an earlier attempt stopped is wrong on a binding
	// that always starts cold.
	CapabilityResume = "resume"

	// CapabilityAppendSystemPrompt — the provider accepts an appended system
	// prompt rather than replacing it. A task relying on the base prompt
	// surviving cannot run where it is overwritten.
	CapabilityAppendSystemPrompt = "append_system_prompt"
)

// ErrCapabilityUnmet reports a requirement the bound provider cannot satisfy.
var ErrCapabilityUnmet = errors.New("provider: binding does not satisfy required capabilities")

// ErrCapabilityUnknown reports a requirement naming a key outside the closed
// vocabulary above.
//
// Distinct from ErrCapabilityUnmet on purpose: an unmet requirement is a
// scheduling fact the operator resolves by choosing another binding, while an
// unknown one is a typo in the plan that no binding will ever satisfy.
var ErrCapabilityUnknown = errors.New("provider: unrecognized capability key")

// BindingCapabilities reports the capability keys a binding satisfies.
//
// Derived from the binding's own config rather than a per-provider table: the
// config already declares these, and a second table would drift the first time
// a provider gained or lost one. Absent means NOT satisfied — callers must not
// read a missing key as "unknown, assume yes".
func BindingCapabilities(cfg BindingConfig) map[string]bool {
	caps := map[string]bool{}
	if cfg.NativeFanout {
		caps[CapabilityNativeFanout] = true
	}
	if cfg.SupportsResume != nil && *cfg.SupportsResume {
		caps[CapabilityResume] = true
	}
	if cfg.UseAppendSystemPrompt != nil && *cfg.UseAppendSystemPrompt {
		caps[CapabilityAppendSystemPrompt] = true
	}
	return caps
}

// KnownCapability reports whether key is in the closed vocabulary.
func KnownCapability(key string) bool {
	switch key {
	case CapabilityNativeFanout, CapabilityResume, CapabilityAppendSystemPrompt:
		return true
	}
	return false
}

// CheckRequirements verifies a binding satisfies every requirement.
//
// Every failing key is named at once. Reporting the first miss alone would turn
// one plan fix into N dispatch cycles, since the operator cannot see the rest
// until the one they fixed stops failing.
func CheckRequirements(binding Binding, requires []string) error {
	if len(requires) == 0 {
		return nil
	}
	caps := BindingCapabilities(binding.Config)

	var unknown, unmet []string
	for _, key := range requires {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch {
		case !KnownCapability(key):
			unknown = append(unknown, key)
		case !caps[key]:
			unmet = append(unmet, key)
		}
	}

	// Unknown keys are reported first and alone: they are a plan defect, and
	// naming them alongside unmet ones invites fixing the wrong thing.
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%w: %s (known: %s)", ErrCapabilityUnknown,
			strings.Join(unknown, ", "),
			strings.Join([]string{
				CapabilityAppendSystemPrompt, CapabilityNativeFanout, CapabilityResume,
			}, ", "))
	}
	if len(unmet) > 0 {
		sort.Strings(unmet)
		return fmt.Errorf("%w: binding %q lacks %s",
			ErrCapabilityUnmet, binding.Name, strings.Join(unmet, ", "))
	}
	return nil
}
