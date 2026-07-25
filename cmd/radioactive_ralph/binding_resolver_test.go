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

// TestStoreBindingResolverProviderPoolRoundRobins proves a plural providers
// config creates independently supervised Ralph workers across the pool. It
// also proves NativeFanout is suppressed for Claude/OpenCode while pooled, so
// one parallel plan group cannot collapse back into one opaque provider turn.
func TestStoreBindingResolverProviderPoolRoundRobins(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)

	projectID, err := st.CreateProject(ctx, "pool-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.SetProjectConfig(ctx, projectID, providersConfigKey, `["claude","codex","opencode"]`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	resolve := storeBindingResolver(st)
	want := []string{"claude", "codex", "opencode", "claude"}
	for i, wantName := range want {
		binding, err := resolve(ctx, projectID, true)
		if err != nil {
			t.Fatalf("resolve binding %d: %v", i, err)
		}
		if binding.Name != wantName {
			t.Errorf("binding %d name = %q, want %q", i, binding.Name, wantName)
		}
		if binding.Config.NativeFanout {
			t.Errorf("binding %d (%s) NativeFanout = true, want false for Ralph-managed pool", i, binding.Name)
		}
	}
}

func TestStoreBindingResolverRejectsInvalidProviderPool(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)

	projectID, err := st.CreateProject(ctx, "bad-pool-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.SetProjectConfig(ctx, projectID, providersConfigKey, `["claude","claude"]`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	_, err = storeBindingResolver(st)(ctx, projectID, true)
	if err == nil {
		t.Fatal("expected duplicate provider pool to fail")
	}
}
