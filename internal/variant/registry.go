package variant

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound indicates a Lookup for an unregistered variant.
var ErrNotFound = errors.New("variant: not found")

var (
	regMu    sync.RWMutex
	registry = make(map[Name]Profile)
)

// Register adds profile to the global registry after validation.
// Intended to be called from variant package init functions.
// Returns an error rather than panicking so tests can exercise
// invalid profiles.
func Register(p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	regMu.Lock()
	defer regMu.Unlock()
	registry[p.Name] = p
	return nil
}

// MustRegister panics on validation failure. Used by built-in variant
// init functions where a validation error is a programmer bug.
func MustRegister(p Profile) {
	if err := Register(p); err != nil {
		panic(fmt.Sprintf("variant.MustRegister(%s): %v", p.Name, err))
	}
}

// Lookup returns the profile for name (case-insensitive match).
// Returns ErrNotFound if name isn't registered.
func Lookup(name string) (Profile, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	norm := Name(strings.ToLower(strings.TrimSpace(name)))
	if p, ok := registry[norm]; ok {
		return p, nil
	}
	return Profile{}, fmt.Errorf("%w: %q", ErrNotFound, name)
}

// All returns every registered profile, sorted by Name.
func All() []Profile {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Profile, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResetRegistryForTesting clears the registry and re-registers the
// built-in variants. Tests that mutate the registry call this from a
// t.Cleanup to avoid bleeding state between tests.
func ResetRegistryForTesting() {
	regMu.Lock()
	registry = make(map[Name]Profile)
	regMu.Unlock()
	// Re-register outside the lock — Register takes its own write lock,
	// and sync.RWMutex is not re-entrant.
	registerBuiltins()
}

func init() {
	registerBuiltins()
}

// registerBuiltins registers all ten variants. Each profile is defined
// in its own file (blue.go, grey.go, ...).
func registerBuiltins() {
	MustRegister(blueProfile())
	MustRegister(greyProfile())
	MustRegister(greenProfile())
	MustRegister(redProfile())
	MustRegister(professorProfile())
	MustRegister(fixitProfile())
	MustRegister(immortalProfile())
	MustRegister(savageProfile())
	MustRegister(oldManProfile())
	MustRegister(worldBreakerProfile())
}
