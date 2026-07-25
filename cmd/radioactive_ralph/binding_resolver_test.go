package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
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
	binding, err := resolve(ctx, projectID, false, orch.BindingDispatch)
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
	binding, err := resolve(ctx, projectID, false, orch.BindingDispatch)
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
		binding, err := resolve(ctx, projectID, true, orch.BindingDispatch)
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

func TestStoreBindingResolverProbeDoesNotConsumePoolCursor(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)
	projectID, err := st.CreateProject(ctx, "probe-pool", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.SetProjectConfig(ctx, projectID, providersConfigKey, `["claude","codex"]`); err != nil {
		t.Fatalf("set provider pool: %v", err)
	}

	resolve := storeBindingResolver(st)
	for i := 0; i < 2; i++ {
		binding, err := resolve(ctx, projectID, true, orch.BindingProbe)
		if err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
		if binding.Name != "claude" {
			t.Fatalf("probe %d selected %q, want claude without consuming the cursor", i, binding.Name)
		}
	}
	first, err := resolve(ctx, projectID, true, orch.BindingDispatch)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if first.Name != "claude" {
		t.Fatalf("first dispatch selected %q, want claude", first.Name)
	}
	nextProbe, err := resolve(ctx, projectID, true, orch.BindingProbe)
	if err != nil {
		t.Fatalf("next probe: %v", err)
	}
	if nextProbe.Name != "codex" {
		t.Fatalf("next probe selected %q, want codex", nextProbe.Name)
	}
}

func TestStoreBindingResolverSingleSelectionPreservesNativeFanout(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "legacy singular", key: providerConfigKey, value: `"claude"`},
		{name: "canonical one element", key: providersConfigKey, value: `["claude"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st := openBindingTestStore(t)
			projectID, err := st.CreateProject(ctx, "single-provider", []store.Fingerprint{
				{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
			})
			if err != nil {
				t.Fatalf("CreateProject: %v", err)
			}
			if err := st.SetProjectConfig(ctx, projectID, tt.key, tt.value); err != nil {
				t.Fatalf("set provider selection: %v", err)
			}

			binding, err := storeBindingResolver(st)(ctx, projectID, true, orch.BindingDispatch)
			if err != nil {
				t.Fatalf("resolve binding: %v", err)
			}
			if binding.Name != "claude" {
				t.Fatalf("binding.Name = %q, want claude", binding.Name)
			}
			if !binding.Config.NativeFanout {
				t.Fatal("single-provider selection disabled Claude native fanout")
			}
		})
	}
}

func TestStoreBindingResolverRejectsWholeInvalidPoolBeforeAssignment(t *testing.T) {
	tests := []struct {
		name string
		pool string
	}{
		{name: "duplicate", pool: `["claude","claude"]`},
		{name: "unknown member", pool: `["claude","not-a-provider","codex"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st := openBindingTestStore(t)
			projectID, err := st.CreateProject(ctx, "bad-pool-project", []store.Fingerprint{
				{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
			})
			if err != nil {
				t.Fatalf("CreateProject: %v", err)
			}
			if err := st.SetProjectConfig(ctx, projectID, providersConfigKey, tt.pool); err != nil {
				t.Fatalf("SetProjectConfig: %v", err)
			}

			resolve := storeBindingResolver(st)
			// Every call must reject the whole pool. The old per-member
			// validation advanced the cursor and could dispatch a valid member
			// on the next tick after encountering an invalid one.
			for attempt := 0; attempt < 3; attempt++ {
				if _, err := resolve(ctx, projectID, true, orch.BindingDispatch); err == nil {
					t.Fatalf("attempt %d unexpectedly assigned a provider", attempt)
				}
			}
		})
	}
}

func TestStoreBindingResolverProjectSingularOverridesUserPool(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)

	userScopeID, err := vconfig.UserScopeProjectID(ctx, st)
	if err != nil {
		t.Fatalf("UserScopeProjectID: %v", err)
	}
	if err := st.SetProjectConfig(ctx, userScopeID, providersConfigKey, `["claude","opencode"]`); err != nil {
		t.Fatalf("set user pool: %v", err)
	}
	projectID, err := st.CreateProject(ctx, "project-override", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// An old DB may still contain the legacy alias. Project scope must win
	// before migration even when the lower user layer uses the canonical key.
	if err := st.SetProjectConfig(ctx, projectID, providerConfigKey, `"codex"`); err != nil {
		t.Fatalf("set legacy project provider: %v", err)
	}

	binding, err := storeBindingResolver(st)(ctx, projectID, true, orch.BindingDispatch)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	if binding.Name != "codex" {
		t.Errorf("binding.Name = %q, want project-scoped codex", binding.Name)
	}
}

func TestProviderResolverProjectStanzaAliasOverridesStoredAndUserPools(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)
	projectID, err := st.CreateProject(ctx, "stanza-override", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.SetProjectConfig(ctx, projectID, providersConfigKey, `["claude","opencode"]`); err != nil {
		t.Fatalf("set stored pool: %v", err)
	}

	userCfg := vconfig.UserConfig{
		Values: map[string]any{
			providersConfigKey: []string{"claude", "opencode"},
		},
		Projects: map[string]map[string]any{
			projectID: {providerConfigKey: "codex"},
		},
	}
	names, pooled, err := resolveProviderNamesFromUserConfig(ctx, st, userCfg, projectID)
	if err != nil {
		t.Fatalf("resolveProviderNamesFromUserConfig: %v", err)
	}
	if pooled {
		t.Fatal("one-provider legacy alias unexpectedly enabled Ralph-managed pooling")
	}
	if len(names) != 1 || names[0] != "codex" {
		t.Errorf("names = %v, want [codex]", names)
	}
}

func TestStoreBindingResolverConcurrentRoundRobinIsBalanced(t *testing.T) {
	ctx := context.Background()
	st := openBindingTestStore(t)
	projectID, err := st.CreateProject(ctx, "concurrent-pool", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	names := []string{"claude", "codex", "opencode"}
	if err := st.SetProjectConfig(ctx, projectID, providersConfigKey, `["claude","codex","opencode"]`); err != nil {
		t.Fatalf("set provider pool: %v", err)
	}

	resolve := storeBindingResolver(st)
	const calls = 300
	counts := make(map[string]int, len(names))
	var countsMu sync.Mutex
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	wg.Add(calls)
	for i := 0; i < calls; i++ {
		go func() {
			defer wg.Done()
			binding, err := resolve(ctx, projectID, true, orch.BindingDispatch)
			if err != nil {
				errs <- err
				return
			}
			if binding.Config.NativeFanout {
				errs <- fmt.Errorf("%s retained NativeFanout in Ralph-managed pool", binding.Name)
				return
			}
			countsMu.Lock()
			counts[binding.Name]++
			countsMu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for _, name := range names {
		if counts[name] != calls/len(names) {
			t.Errorf("provider %q calls = %d, want %d; counts=%v", name, counts[name], calls/len(names), counts)
		}
	}
}
