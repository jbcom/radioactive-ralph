package provider

import (
	"errors"
	"testing"
)

// TestBindingCapabilitiesReportsWhatTheConfigDeclares maps a binding's config
// onto the capability keys a plan can require.
//
// Config-driven rather than a hardcoded per-provider table: BindingConfig
// already declares these, and a second table would drift the first time a
// provider gains or loses one.
func TestBindingCapabilitiesReportsWhatTheConfigDeclares(t *testing.T) {
	yes := true
	caps := BindingCapabilities(BindingConfig{
		Type:                  "claude",
		NativeFanout:          true,
		SupportsResume:        &yes,
		UseAppendSystemPrompt: &yes,
	})
	for _, want := range []string{CapabilityNativeFanout, CapabilityResume, CapabilityAppendSystemPrompt} {
		if !caps[want] {
			t.Errorf("capability %q missing from %+v", want, caps)
		}
	}
}

// TestBindingCapabilitiesOmitsWhatIsNotDeclared is the half that makes the
// check meaningful: a capability the binding does not have must be absent, not
// merely false-y in a way a caller might read as "unknown, assume yes".
func TestBindingCapabilitiesOmitsWhatIsNotDeclared(t *testing.T) {
	no := false
	caps := BindingCapabilities(BindingConfig{
		Type:           "codex",
		NativeFanout:   false,
		SupportsResume: &no,
	})
	if caps[CapabilityNativeFanout] {
		t.Error("native_fanout reported for a binding that declares it false")
	}
	if caps[CapabilityResume] {
		t.Error("resume reported for a binding that declares it false")
	}
}

// TestUnmetRequirementsNamesEveryMissingKey — an operator fixing a plan needs
// the full list, not the first failure. Reporting one key at a time turns one
// fix into N dispatch cycles.
func TestUnmetRequirementsNamesEveryMissingKey(t *testing.T) {
	binding := Binding{Name: "codex", Config: BindingConfig{Type: "codex"}}
	err := CheckRequirements(binding, []string{CapabilityNativeFanout, CapabilityResume})
	if err == nil {
		t.Fatal("a binding with neither capability satisfied both requirements")
	}
	if !errors.Is(err, ErrCapabilityUnmet) {
		t.Fatalf("err = %v, want ErrCapabilityUnmet", err)
	}
	for _, key := range []string{CapabilityNativeFanout, CapabilityResume} {
		if !contains(err.Error(), key) {
			t.Errorf("err = %v, want it to name %q — an operator needs every missing key at once", err, key)
		}
	}
}

// TestUnknownRequirementIsRefused fails closed on a typo. Silently ignoring an
// unrecognized key would let "nativefanout" pass a task straight through to a
// provider that cannot fan out — the exact outcome `requires` exists to stop.
func TestUnknownRequirementIsRefused(t *testing.T) {
	binding := Binding{Name: "claude", Config: BindingConfig{Type: "claude", NativeFanout: true}}
	err := CheckRequirements(binding, []string{"nativefanout"})
	if err == nil {
		t.Fatal("an unrecognized capability key was accepted; a typo must not silently pass")
	}
	if !errors.Is(err, ErrCapabilityUnknown) {
		t.Fatalf("err = %v, want ErrCapabilityUnknown, distinct from a merely unmet one", err)
	}
}

// TestNoRequirementsAlwaysPasses keeps the common case free: an unannotated
// task requires nothing.
func TestNoRequirementsAlwaysPasses(t *testing.T) {
	binding := Binding{Name: "codex", Config: BindingConfig{Type: "codex"}}
	if err := CheckRequirements(binding, nil); err != nil {
		t.Fatalf("a task requiring nothing was refused: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
