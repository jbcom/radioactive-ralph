package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

func openBindingTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), store.Options{
		DSN: store.DSN(filepath.Join(t.TempDir(), "store.db")),
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestStoreBindingResolverHonorsProjectConfig proves stored virtual config
// selects the provider: a project configured with provider="codex" resolves
// to the codex binding, not the built-in claude default. Before this wiring
// the supervisor always ran claude regardless of stored config.
func TestStoreBindingResolverHonorsProjectConfig(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)

	projectID, err := st.CreateProject(ctx, "cfg-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Stored config values are JSON-encoded (see store.SetProjectConfig /
	// vconfig.loadStoreConfig); a string value is a quoted JSON string.
	if err := st.SetProjectConfig(ctx, projectID, providerConfigKey, `"codex"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	resolve := storeBindingResolver(st)
	binding, err := resolve(ctx, projectID, false)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.Name != "codex" {
		t.Errorf("binding.Name = %q, want %q (stored config must select the provider)", binding.Name, "codex")
	}
	if binding.Config.Type != "codex" {
		t.Errorf("binding.Config.Type = %q, want codex", binding.Config.Type)
	}
}

// TestStoreBindingResolverDefaultsToClaude proves a project with no provider
// configured falls back to the built-in claude binding (ResolveBinding's own
// default), so an unconfigured project still dispatches.
func TestStoreBindingResolverDefaultsToClaude(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)

	projectID, err := st.CreateProject(ctx, "default-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	resolve := storeBindingResolver(st)
	binding, err := resolve(ctx, projectID, false)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.Name != "claude" {
		t.Errorf("binding.Name = %q, want claude (default for an unconfigured project)", binding.Name)
	}
}
