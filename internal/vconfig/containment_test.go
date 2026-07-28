package vconfig

import "testing"

// TestContainProviderWritesDefaultsOn pins the flipped default.
//
// It defaulted OFF while the guarantee was unproven, and that caution was
// JUSTIFIED: the first attempt to flip it broke codex and opencode, both of
// which failed to complete a turn under containment.
//
// It is no longer a guess. A binding now declares whether it can run confined
// (BindingConfig.SupportsContainment), so a provider that cannot is SPARED
// rather than broken, and every shipped provider completes a real turn with
// containment enabled. Enforcement is proven separately by
// TestE2E_ContainedTurnCannotWriteOutsideTheProject, which fails when
// containment is disabled and runs in a required check.
//
// Off is still one config key away for a project whose provider needs a wider
// root; on-by-default only changes which choice requires the key.
func TestContainProviderWritesDefaultsOn(t *testing.T) {
	if !ContainProviderWrites(ProjectConfig{Values: map[string]any{}}) {
		t.Error("containment defaulted OFF with no key set; a security boundary that " +
			"applies only when someone remembers to ask for it protects nobody by default")
	}
	if !ContainProviderWrites(ProjectConfig{}) {
		t.Error("a nil Values map disabled containment")
	}
}

// TestContainProviderWritesReadsTheOperatorsIntent covers the shapes a TOML or
// JSON layer actually produces. A bool from TOML and a string from a JSON-ish
// store layer both mean the same thing to the person who typed it.
//
// An EXPLICIT false still wins: the flip changed the default, not the
// operator's ability to override it.
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
		{"unparseable string", "yes-please", true},
		{"nil", nil, true},
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

// TestUnparseableValueDoesNotDisableContainment is called out separately
// because the failure direction matters, and it INVERTED with the default.
//
// While absent meant off, a typo silently ENABLING the boundary would surface
// as unexplained provider write failures far from their cause. Now that absent
// means on, a typo must not silently REMOVE a boundary the operator believes is
// active — the failure nobody notices until it matters.
//
// Both readings choose "behave like the absent key". Only the key's meaning
// changed, so this is the old test with its direction flipped, not a weakened
// version of it.
func TestUnparseableValueDoesNotDisableContainment(t *testing.T) {
	for _, bad := range []any{"tru", "1.5", []any{true}, map[string]any{"x": 1}} {
		if !ContainProviderWrites(ProjectConfig{
			Values: map[string]any{ContainProviderWritesKey: bad},
		}) {
			t.Errorf("%#v disabled containment; a malformed value must behave like an "+
				"absent key, or a typo silently strips a security boundary", bad)
		}
	}
}

// TestExplicitFalseStillDisablesContainment is the opt-out the flip depends on
// being real. A provider that legitimately writes outside the checkout must
// remain runnable without patching Ralph.
func TestExplicitFalseStillDisablesContainment(t *testing.T) {
	for _, off := range []any{false, "false", "0", "FALSE"} {
		if ContainProviderWrites(ProjectConfig{
			Values: map[string]any{ContainProviderWritesKey: off},
		}) {
			t.Errorf("%#v did NOT disable containment; on-by-default is defensible "+
				"only while an operator can still turn it off", off)
		}
	}
}
