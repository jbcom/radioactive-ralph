package vconfig

import (
	"context"
	"testing"
)

// TestEffectiveProjectChangePersists verifies ModeChange merges the
// project-config-file into the returned config AND persists each merged
// key back to the store.
func TestEffectiveProjectChangePersists(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "change-project")

	if err := s.SetProjectConfig(ctx, projectID, "model", `"original-model"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	projFile := writeTOML(t, `model = "changed-model"`)

	base := ProjectConfig{Values: map[string]any{"model": "original-model"}}

	eff, err := EffectiveProject(ctx, s, base, projectID, projFile, ModeChange)
	if err != nil {
		t.Fatalf("EffectiveProject: %v", err)
	}
	if got := eff.Values["model"]; got != "changed-model" {
		t.Errorf("effective model = %v, want changed-model", got)
	}

	// Verify persistence: re-read directly from the store.
	stored, err := s.GetProjectConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if stored["model"] != `"changed-model"` {
		t.Errorf("stored model = %q, want %q (ModeChange must persist)", stored["model"], `"changed-model"`)
	}
}

// TestEffectiveProjectOverrideDoesNotPersist verifies ModeOverride merges
// the project-config-file into the returned config but leaves the store
// untouched.
func TestEffectiveProjectOverrideDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "override-project")

	if err := s.SetProjectConfig(ctx, projectID, "model", `"original-model"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	projFile := writeTOML(t, `model = "overridden-model"`)

	base := ProjectConfig{Values: map[string]any{"model": "original-model"}}

	eff, err := EffectiveProject(ctx, s, base, projectID, projFile, ModeOverride)
	if err != nil {
		t.Fatalf("EffectiveProject: %v", err)
	}
	if got := eff.Values["model"]; got != "overridden-model" {
		t.Errorf("effective model = %v, want overridden-model", got)
	}

	// Verify no persistence: store must retain the original value.
	stored, err := s.GetProjectConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if stored["model"] != `"original-model"` {
		t.Errorf("stored model = %q, want %q (ModeOverride must NOT persist)", stored["model"], `"original-model"`)
	}
}

// TestEffectiveProjectNoFileReturnsBaseUnchanged verifies an empty
// projectConfigFile is a no-op passthrough regardless of mode.
func TestEffectiveProjectNoFileReturnsBaseUnchanged(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "no-file-project")

	base := ProjectConfig{Values: map[string]any{"model": "base-model"}}

	eff, err := EffectiveProject(ctx, s, base, projectID, "", ModeChange)
	if err != nil {
		t.Fatalf("EffectiveProject: %v", err)
	}
	if got := eff.Values["model"]; got != "base-model" {
		t.Errorf("effective model = %v, want base-model unchanged", got)
	}
}
