package vconfig

import "testing"

// TestContainProviderWritesDefaultsOff keeps a config-driven switch from
// silently changing behavior for existing deployments.
//
// Enabling containment changes what a provider is permitted to do: one that
// legitimately writes to a shared cache outside the checkout starts failing.
// That has to be an operator's decision, so an absent key means OFF.
func TestContainProviderWritesDefaultsOff(t *testing.T) {
	if ContainProviderWrites(ProjectConfig{Values: map[string]any{}}) {
		t.Error("containment defaulted ON with no key set; an upgrade would change " +
			"what every existing deployment's provider may do")
	}
	if ContainProviderWrites(ProjectConfig{}) {
		t.Error("a nil Values map enabled containment")
	}
}

// TestContainProviderWritesReadsTheOperatorsIntent covers the shapes a TOML or
// JSON layer actually produces. A bool from TOML and a string from a JSON-ish
// store layer both mean the same thing to the person who typed it.
func TestContainProviderWritesReadsTheOperatorsIntent(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		want bool
	}{
		{"toml bool true", true, true},
		{"toml bool false", false, false},
		{"string true", "true", true},
		{"string false", "false", false},
		{"unparseable string", "yes-please", false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ContainProviderWrites(ProjectConfig{
				Values: map[string]any{ContainProviderWritesKey: tc.val},
			})
			if got != tc.want {
				t.Errorf("ContainProviderWrites(%v) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

// TestUnparseableValueDoesNotEnableContainment is called out separately because
// the failure direction matters.
//
// A typo must not silently ENABLE a boundary that makes provider writes fail —
// that would look like a provider bug, far from its actual cause. Defaulting a
// malformed value to off keeps a typo behaving like the absent key it resembles.
func TestUnparseableValueDoesNotEnableContainment(t *testing.T) {
	for _, bad := range []any{"tru", "1.5", []any{true}, map[string]any{"x": 1}} {
		if ContainProviderWrites(ProjectConfig{
			Values: map[string]any{ContainProviderWritesKey: bad},
		}) {
			t.Errorf("%#v enabled containment; a malformed value must behave like an "+
				"absent key, or a typo surfaces as unexplained provider write failures", bad)
		}
	}
}
