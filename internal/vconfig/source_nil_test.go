package vconfig

import (
	"context"
	"testing"
)

// nilableSource is a custom ConfigSource whose typed-nil pointer satisfies the
// interface. isNilConfigSource only special-cased *StoreConfigSource, so a
// typed-nil of any OTHER implementation passed the guard and panicked on the
// first method call — which is exactly the shape the IPC-backed source in
// cmd/radioactive_ralph has.
type nilableSource struct{ values map[string]string }

func (s *nilableSource) UserScopeProject(context.Context) (string, error) { return "u", nil }

func (s *nilableSource) ProjectConfigValues(_ context.Context, _ string) (map[string]string, error) {
	return s.values, nil // nil receiver panics here
}

func (s *nilableSource) ApplyProjectConfigValues(
	_ context.Context, _ string, _ map[string]string, _ []string,
) error {
	return nil
}

// TestResolveProjectsFromToleratesANilSource is the reported Major on #233.
//
// ResolveProjectsFrom dereferenced the source unconditionally, so a caller with
// no store — the documented "user overlay only" case — panicked instead of
// getting the user `projects:` overlay it asked for.
func TestResolveProjectsFromToleratesANilSource(t *testing.T) {
	userCfg := UserConfig{Projects: map[string]map[string]any{
		"p1": {"model": "opus"},
	}}
	got, err := ResolveProjectsFrom(context.Background(), nil, userCfg, "p1")
	if err != nil {
		t.Fatalf("ResolveProjectsFrom with a nil source: %v", err)
	}
	if got.Values["model"] != "opus" {
		t.Fatalf("Values = %+v, want the user overlay applied when there is no "+
			"source to layer under it", got.Values)
	}
}

// TestResolveProjectsFromToleratesATypedNilSource covers the shape the
// *StoreConfigSource-only check missed: any OTHER implementation's typed nil.
func TestResolveProjectsFromToleratesATypedNilSource(t *testing.T) {
	var src *nilableSource // typed nil: non-nil interface, nil pointer
	userCfg := UserConfig{Projects: map[string]map[string]any{
		"p1": {"model": "sonnet"},
	}}
	got, err := ResolveProjectsFrom(context.Background(), src, userCfg, "p1")
	if err != nil {
		t.Fatalf("ResolveProjectsFrom with a typed-nil source: %v", err)
	}
	if got.Values["model"] != "sonnet" {
		t.Fatalf("Values = %+v, want the user overlay", got.Values)
	}
}

// TestIsNilConfigSourceRecognizesAnyTypedNil is the unit-level statement of the
// same rule: the guard must not be specific to one implementation.
func TestIsNilConfigSourceRecognizesAnyTypedNil(t *testing.T) {
	var custom *nilableSource
	if !isNilConfigSource(custom) {
		t.Error("a typed-nil custom source was not recognized as nil; it would " +
			"pass the guard and panic on first use")
	}
	if isNilConfigSource(&nilableSource{}) {
		t.Error("a real source was reported nil; config would be silently ignored")
	}
}
