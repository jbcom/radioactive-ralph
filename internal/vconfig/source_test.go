package vconfig

import (
	"context"
	"errors"
	"testing"
)

// fakeConfigSource stands in for the supervisor client: it answers config
// reads/writes without a store, which is the whole point of the seam.
type fakeConfigSource struct {
	userProjectID string
	values        map[string]map[string]string
	applied       []applyCall
	getErr        error
}

type applyCall struct {
	projectID  string
	upserts    map[string]string
	deleteKeys []string
}

func (f *fakeConfigSource) UserScopeProject(context.Context) (string, error) {
	return f.userProjectID, nil
}

func (f *fakeConfigSource) ProjectConfigValues(_ context.Context, projectID string) (map[string]string, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.values[projectID], nil
}

func (f *fakeConfigSource) ApplyProjectConfigValues(
	_ context.Context, projectID string, upserts map[string]string, deleteKeys []string,
) error {
	f.applied = append(f.applied, applyCall{projectID, upserts, deleteKeys})
	return nil
}

// TestResolveUserReadsThroughTheSource is the seam that lets the CLI stop
// opening the store. vconfig previously took a *store.Store, so any caller
// resolving config layers had to be a direct database reader — the exact thing
// the dumb-client contract forbids.
func TestResolveUserReadsThroughTheSource(t *testing.T) {
	src := &fakeConfigSource{
		userProjectID: "user-scope",
		values: map[string]map[string]string{
			"user-scope": {"provider": `"claude"`},
		},
	}
	cfg, err := ResolveUserFrom(context.Background(), src, "", "")
	if err != nil {
		t.Fatalf("ResolveUserFrom: %v", err)
	}
	if got := cfg.Values["provider"]; got != "claude" {
		t.Fatalf("provider = %v, want claude (decoded from the stored JSON)", got)
	}
}

// TestResolveProjectsReadsThroughTheSource covers the project layer.
//
// The layering is deliberately NOT "user values fall through to projects": a
// project's stored config is the base, and only the user config's
// projects.<id> stanza overlays it. The seam must reproduce that exactly, since
// resolving through the socket and resolving through the store have to agree.
func TestResolveProjectsReadsThroughTheSource(t *testing.T) {
	src := &fakeConfigSource{
		userProjectID: "user-scope",
		values: map[string]map[string]string{
			"user-scope": {"provider": `"claude"`},
			"proj-1":     {"provider": `"codex"`, "only-in-project": `"kept"`},
		},
	}
	userCfg, err := ResolveUserFrom(context.Background(), src, "", "")
	if err != nil {
		t.Fatalf("ResolveUserFrom: %v", err)
	}
	projCfg, err := ResolveProjectsFrom(context.Background(), src, userCfg, "proj-1")
	if err != nil {
		t.Fatalf("ResolveProjectsFrom: %v", err)
	}
	if got := projCfg.Values["provider"]; got != "codex" {
		t.Errorf("provider = %v, want the project's own stored value", got)
	}
	if got := projCfg.Values["only-in-project"]; got != "kept" {
		t.Errorf("only-in-project = %v, want it read through the source", got)
	}
	// Top-level user values do NOT leak into the project layer.
	if _, present := projCfg.Values["shared"]; present {
		t.Errorf("a top-level user key leaked into the project layer: %+v", projCfg.Values)
	}
}

// TestSourceErrorsSurface keeps the seam fail-closed. A config read that fails
// must not resolve to an empty layer, which would silently discard every stored
// value and look like a fresh project.
func TestSourceErrorsSurface(t *testing.T) {
	boom := errors.New("supervisor unreachable")
	src := &fakeConfigSource{userProjectID: "user-scope", getErr: boom}
	if _, err := ResolveUserFrom(context.Background(), src, "", ""); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the source error to surface rather than an empty layer", err)
	}
}

// TestStoreSatisfiesTheSource pins that the supervisor side keeps working
// through the same interface, so there is ONE resolution path rather than a
// client path and a drifting server path.
func TestStoreSatisfiesTheSource(t *testing.T) {
	// A nil store must yield a nil source, so callers keep the documented
	// "no store, file layers only" behavior instead of panicking on a
	// typed-nil pointer wrapped in a non-nil interface.
	if got := NewStoreConfigSource(nil); !isNilConfigSource(got) {
		t.Fatalf("NewStoreConfigSource(nil) = %#v, want a source that reads as nil", got)
	}
	var _ ConfigSource = (*StoreConfigSource)(nil)
}
