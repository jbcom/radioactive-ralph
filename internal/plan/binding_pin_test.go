package plan

import "testing"

// TestBindingPinAndProvidersCoexistAtImport pins that plan validation does NOT
// try to adjudicate a pin against a `providers` list.
//
// An earlier version rejected providers:["codex"] + binding.provider:"claude"
// as contradictory. That check was WRONG, not merely strict: `providers`
// entries may name a configured ALIAS, and the alias-to-type mapping lives in
// operator config that ValidateForImport never sees. providers:["reviewer"]
// with binding.provider:"codex" is perfectly consistent when `reviewer` is a
// codex binding, and refusing it would reject a valid plan on a string
// comparison that cannot know the answer.
//
// The pin is enforced at DISPATCH instead, where the resolved binding is known.
func TestBindingPinAndProvidersCoexistAtImport(t *testing.T) {
	for name, md := range map[string]string{
		"same name": "# P\n\n- w\n\n   ```ralph-task\n" +
			`   {"id":"a","providers":["codex"],"binding":{"provider":"codex"}}` + "\n   ```\n",
		"alias and type differ": "# P\n\n- w\n\n   ```ralph-task\n" +
			`   {"id":"a","providers":["reviewer"],"binding":{"provider":"codex"}}` + "\n   ```\n",
		"pin alone": "# P\n\n- w\n\n   ```ralph-task\n" +
			`   {"id":"a","binding":{"provider":"codex"}}` + "\n   ```\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateForImport([]byte(md)); err != nil {
				t.Fatalf("import rejected a plan it cannot adjudicate: %v", err)
			}
		})
	}
}

// TestPinnedProviderTypeIsSeparateFromAllowedProviders is the wiring assertion.
//
// The pin must NOT be folded into AllowedProviders: CheckAllowedProviders
// matches an entry against the binding alias OR its type, so folding it there
// let an alias merely NAMED "codex" -- backed by type "claude" -- satisfy the
// pin and run the task on Claude.
func TestPinnedProviderTypeIsSeparateFromAllowedProviders(t *testing.T) {
	m := &TaskMetadata{}
	m.Binding.Provider = " codex "

	if got := m.PinnedProviderType(); got != "codex" {
		t.Fatalf("PinnedProviderType() = %q, want \"codex\" (trimmed)", got)
	}
	if got := m.AllowedProviders(); len(got) != 0 {
		t.Fatalf("AllowedProviders() = %v, want empty: a pin folded into the "+
			"allowed-set is matched against the binding ALIAS too, so a lookalike "+
			"alias would satisfy it", got)
	}
}

// TestPinnedProviderTypeToleratesNil keeps the accessor usable at call sites
// that do not nil-check metadata, matching AllowedProviders.
func TestPinnedProviderTypeToleratesNil(t *testing.T) {
	var m *TaskMetadata
	if got := m.PinnedProviderType(); got != "" {
		t.Fatalf("PinnedProviderType() on nil = %q, want \"\"", got)
	}
}
