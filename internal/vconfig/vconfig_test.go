package vconfig

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: store.DSN(filepath.Join(t.TempDir(), "vconfig.db"))})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustCreateProject(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	id, err := s.CreateProject(context.Background(), name, []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/tmp/" + name},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return id
}

func writeTOML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cfg.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	return path
}

// TestUserScopeProjectIDIdempotent verifies the reserved user-scope project
// row is created once and resolved (not recreated) on subsequent calls.
func TestUserScopeProjectIDIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	id1, err := UserScopeProjectID(ctx, s)
	if err != nil {
		t.Fatalf("UserScopeProjectID: %v", err)
	}
	if id1 == "" {
		t.Fatal("UserScopeProjectID returned empty id")
	}

	id2, err := UserScopeProjectID(ctx, s)
	if err != nil {
		t.Fatalf("UserScopeProjectID (2nd call): %v", err)
	}
	if id1 != id2 {
		t.Fatalf("UserScopeProjectID not idempotent: %q vs %q", id1, id2)
	}
}

// TestResolveUserTwoLayerPrecedence exercises the full precedence chain:
// DB user-scope config (lowest, above defaults) < configFile < userConfigFile
// (highest). Each layer sets the same key to a different value; the highest
// layer present must win, and a key set only in the DB must still surface.
func TestResolveUserTwoLayerPrecedence(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	userScopeID, err := UserScopeProjectID(ctx, s)
	if err != nil {
		t.Fatalf("UserScopeProjectID: %v", err)
	}

	// DB layer: sets "model" and a DB-only key "db_only".
	if err := s.SetProjectConfig(ctx, userScopeID, "model", `"db-model"`); err != nil {
		t.Fatalf("SetProjectConfig model: %v", err)
	}
	if err := s.SetProjectConfig(ctx, userScopeID, "db_only", `"db-value"`); err != nil {
		t.Fatalf("SetProjectConfig db_only: %v", err)
	}

	// configFile layer: overrides "model", sets "config_file_only".
	configFile := writeTOML(t, `
model = "config-file-model"
config_file_only = "cf-value"
`)

	// userConfigFile layer: overrides "model" again (highest precedence).
	userConfigFile := writeTOML(t, `
model = "user-config-file-model"
`)

	userCfg, err := ResolveUser(ctx, s, configFile, userConfigFile)
	if err != nil {
		t.Fatalf("ResolveUser: %v", err)
	}

	if got := userCfg.Values["model"]; got != "user-config-file-model" {
		t.Errorf("model = %v, want user-config-file-model (highest layer must win)", got)
	}
	if got := userCfg.Values["db_only"]; got != "db-value" {
		t.Errorf("db_only = %v, want db-value (DB layer must surface when no file overrides it)", got)
	}
	if got := userCfg.Values["config_file_only"]; got != "cf-value" {
		t.Errorf("config_file_only = %v, want cf-value", got)
	}
}

// TestResolveUserConfigFileOnly verifies configFile alone (no
// userConfigFile, no DB values) still resolves correctly — each layer is
// independently optional.
func TestResolveUserConfigFileOnly(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	configFile := writeTOML(t, `model = "only-config-file"`)

	userCfg, err := ResolveUser(ctx, s, configFile, "")
	if err != nil {
		t.Fatalf("ResolveUser: %v", err)
	}
	if got := userCfg.Values["model"]; got != "only-config-file" {
		t.Errorf("model = %v, want only-config-file", got)
	}
}

// TestResolveUserExtractsProjectsStanza verifies the projects: stanza is
// pulled out into UserConfig.Projects (keyed by project id) and removed
// from the top-level Values.
func TestResolveUserExtractsProjectsStanza(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	configFile := writeTOML(t, `
model = "top-level-model"

[projects.proj-a]
model = "proj-a-model"
extra_key = "proj-a-extra"
`)

	userCfg, err := ResolveUser(ctx, s, configFile, "")
	if err != nil {
		t.Fatalf("ResolveUser: %v", err)
	}

	if _, ok := userCfg.Values["projects"]; ok {
		t.Errorf("Values still contains top-level %q key; want it extracted into Projects", "projects")
	}
	projA, ok := userCfg.Projects["proj-a"]
	if !ok {
		t.Fatalf("Projects[proj-a] missing; got %+v", userCfg.Projects)
	}
	if projA["model"] != "proj-a-model" {
		t.Errorf("Projects[proj-a][model] = %v, want proj-a-model", projA["model"])
	}
	if projA["extra_key"] != "proj-a-extra" {
		t.Errorf("Projects[proj-a][extra_key] = %v, want proj-a-extra", projA["extra_key"])
	}
}

// TestResolveProjectsOverlaysUserStanzaOnDB verifies ResolveProjects layers
// the projects: stanza (from UserConfig) on top of the DB project config,
// with the stanza winning on overlapping keys and DB-only keys surviving.
func TestResolveProjectsOverlaysUserStanzaOnDB(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "proj-overlay")

	if err := s.SetProjectConfig(ctx, projectID, "model", `"db-model"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}
	if err := s.SetProjectConfig(ctx, projectID, "db_only", `"db-value"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	userCfg := UserConfig{
		Values: map[string]any{},
		Projects: map[string]map[string]any{
			projectID: {"model": "stanza-model"},
		},
	}

	projCfg, err := ResolveProjects(ctx, s, userCfg, projectID)
	if err != nil {
		t.Fatalf("ResolveProjects: %v", err)
	}

	if got := projCfg.Values["model"]; got != "stanza-model" {
		t.Errorf("model = %v, want stanza-model (projects: stanza must win over DB)", got)
	}
	if got := projCfg.Values["db_only"]; got != "db-value" {
		t.Errorf("db_only = %v, want db-value (DB-only key must survive overlay)", got)
	}
}
