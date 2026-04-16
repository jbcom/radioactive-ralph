package variant

import (
	"errors"
	"testing"
)

// mustLookup wraps Lookup with a t.Fatalf so test bodies don't have to
// keep handling an error that, for built-in variants, is a test setup
// bug rather than a meaningful assertion.
func mustLookup(t *testing.T, name string) Profile {
	t.Helper()
	p, err := Lookup(name)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", name, err)
	}
	return p
}

func TestLookupAllTenVariants(t *testing.T) {
	for _, name := range allVariantNames {
		t.Run(string(name), func(t *testing.T) {
			p := mustLookup(t, string(name))
			if p.Name != name {
				t.Errorf("Lookup(%q).Name = %q", name, p.Name)
			}
		})
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	for _, form := range []string{"green", "Green", "  GREEN  ", "GrEeN"} {
		if _, err := Lookup(form); err != nil {
			t.Errorf("Lookup(%q): %v", form, err)
		}
	}
}

func TestLookupNotFound(t *testing.T) {
	_, err := Lookup("purple")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAllReturnsAllTenProfiles(t *testing.T) {
	list := All()
	if len(list) != 10 {
		t.Fatalf("All() returned %d profiles, want 10", len(list))
	}
	seen := make(map[Name]bool)
	for _, p := range list {
		seen[p.Name] = true
	}
	for _, name := range allVariantNames {
		if !seen[name] {
			t.Errorf("variant %q missing from All()", name)
		}
	}
}

// TestAllIsSortedByName confirms the sort.Slice switch in registry.go
// preserves the alphabetical ordering contract callers rely on.
func TestAllIsSortedByName(t *testing.T) {
	list := All()
	for i := 1; i < len(list); i++ {
		if list[i-1].Name > list[i].Name {
			t.Errorf("All() not sorted: %q > %q at index %d",
				list[i-1].Name, list[i].Name, i)
		}
	}
}

func TestRegisterInvalidProfileReturnsError(t *testing.T) {
	err := Register(Profile{
		Name:            "x",
		AttachedAllowed: true,
		DurableAllowed:  true,
		Isolation:       IsolationShared,
		ToolAllowlist:   []string{ToolWrite},
	})
	if err == nil {
		t.Fatal("expected error for invalid profile")
	}
}

func TestMustRegisterPanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	MustRegister(Profile{})
}

func TestResetRegistry(t *testing.T) {
	extra := Profile{
		Name:            "extra",
		AttachedAllowed: true,
		DurableAllowed:  true,
		Isolation:       IsolationShared,
		ToolAllowlist:   []string{ToolRead},
	}
	if err := Register(extra); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := Lookup("extra"); err != nil {
		t.Errorf("extra not found after register: %v", err)
	}
	ResetRegistryForTesting()
	if _, err := Lookup("extra"); err == nil {
		t.Error("extra should be gone after reset")
	}
	// All ten built-ins survive reset.
	for _, name := range allVariantNames {
		if _, err := Lookup(string(name)); err != nil {
			t.Errorf("built-in %q should survive reset: %v", name, err)
		}
	}
}
