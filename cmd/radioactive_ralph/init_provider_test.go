package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

func writeRawProjectConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	return path
}

func runProjectInit(t *testing.T, configPath string) error {
	t.Helper()
	cmd := newTestRootCmd(context.Background())
	cmd.SetArgs([]string{"--init", "--project-config-file", configPath})
	return cmd.Execute()
}

func TestInitProviderSelectionReplacesPoolAndSingleWithoutStaleAlias(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	chdir(t, t.TempDir())

	if err := runProjectInit(t, writeRawProjectConfig(t,
		`providers = ["claude", "codex"]`+"\n",
	)); err != nil {
		t.Fatalf("initialize pool: %v", err)
	}
	assertStoredProviders(t, stateDir, []string{"claude", "codex"})

	// Legacy ingress is normalized and deliberately replaces the prior pool
	// without requiring --force-override.
	if err := runProjectInit(t, writeRawProjectConfig(t,
		`provider = "opencode"`+"\n",
	)); err != nil {
		t.Fatalf("replace pool with singular alias: %v", err)
	}
	assertStoredProviders(t, stateDir, []string{"opencode"})

	if err := runProjectInit(t, writeRawProjectConfig(t,
		`providers = ["codex", "claude"]`+"\n",
	)); err != nil {
		t.Fatalf("replace singular with pool: %v", err)
	}
	assertStoredProviders(t, stateDir, []string{"codex", "claude"})
}

func TestInitRejectsInvalidProviderSelectionBeforeAnyConfigPersistence(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	chdir(t, t.TempDir())

	err := runProjectInit(t, writeRawProjectConfig(t, `
providers = ["claude", "not-a-provider"]
model = "must-not-persist"
`))
	if err == nil {
		t.Fatal("invalid provider pool: want error")
	}
	if !strings.Contains(err.Error(), "not-a-provider") {
		t.Fatalf("error = %v, want invalid provider name", err)
	}

	cfg := storedConfigForCurrentProject(t, stateDir)
	if _, exists := cfg["model"]; exists {
		t.Errorf("model persisted despite provider validation failure: %+v", cfg)
	}
	if _, exists := cfg[providersConfigKey]; exists {
		t.Errorf("providers persisted despite validation failure: %+v", cfg)
	}
}

func TestInitRejectsBothProviderKeysBeforePersistence(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	chdir(t, t.TempDir())

	err := runProjectInit(t, writeRawProjectConfig(t, `
provider = "codex"
providers = ["claude"]
model = "must-not-persist"
`))
	if err == nil {
		t.Fatal("both provider keys: want error")
	}
	if !strings.Contains(err.Error(), "cannot both be set") {
		t.Fatalf("error = %v, want both-key diagnostic", err)
	}
	if cfg := storedConfigForCurrentProject(t, stateDir); len(cfg) != 0 {
		t.Errorf("config persisted despite ambiguous provider selection: %+v", cfg)
	}
}

func assertStoredProviders(t *testing.T, stateDir string, want []string) {
	t.Helper()
	cfg := storedConfigForCurrentProject(t, stateDir)
	if _, exists := cfg[providerConfigKey]; exists {
		t.Errorf("legacy provider key remained stored: %+v", cfg)
	}
	var got []string
	if err := json.Unmarshal([]byte(cfg[providersConfigKey]), &got); err != nil {
		t.Fatalf("decode stored providers %q: %v", cfg[providersConfigKey], err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored providers = %v, want %v", got, want)
	}
}

func storedConfigForCurrentProject(t *testing.T, stateDir string) map[string]string {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	fingerprints, err := store.Fingerprints(ctx, cwd)
	if err != nil {
		t.Fatalf("Fingerprints: %v", err)
	}
	projectID, found, err := st.ResolveProject(ctx, fingerprints)
	if err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if !found {
		t.Fatal("project not found after --init")
	}
	cfg, err := st.GetProjectConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	return cfg
}
