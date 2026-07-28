package supervisor

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestHandleProjectConfigGetReturnsStoredValues is the read half of the
// config-apply surface. `init` needs the project's stored config to resolve
// vconfig layers, and reading it directly from SQLite is the last thing keeping
// the CLI a second writer to a supervisor-owned database.
func TestHandleProjectConfigGetReturnsStoredValues(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "cfg-read",
		[]store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := sup.store.SetProjectConfig(ctx, projectID, "provider", `"claude"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	reply, err := sup.HandleProjectConfigGet(ctx, ipc.ProjectConfigGetArgs{Project: projectID})
	if err != nil {
		t.Fatalf("HandleProjectConfigGet: %v", err)
	}
	if reply.Values["provider"] != `"claude"` {
		t.Fatalf("values = %+v, want the stored provider", reply.Values)
	}
}

// TestHandleProjectConfigApplyUpsertsAndDeletes is the write half. Both
// operations land in ONE call because vconfig's override path computes an
// upsert set and a delete set together — splitting them across two round trips
// would let a crash between them leave the project half-configured.
func TestHandleProjectConfigApplyUpsertsAndDeletes(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "cfg-apply",
		[]store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := sup.store.SetProjectConfig(ctx, projectID, "stale", `"gone"`); err != nil {
		t.Fatalf("SetProjectConfig: %v", err)
	}

	if err := sup.HandleProjectConfigApply(ctx, ipc.ProjectConfigApplyArgs{
		Project:    projectID,
		Upserts:    map[string]string{"provider": `"codex"`},
		DeleteKeys: []string{"stale"},
	}); err != nil {
		t.Fatalf("HandleProjectConfigApply: %v", err)
	}

	got, err := sup.store.GetProjectConfig(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if got["provider"] != `"codex"` {
		t.Errorf("upsert did not land: %+v", got)
	}
	if _, present := got["stale"]; present {
		t.Errorf("delete did not land: %+v", got)
	}
}

// TestHandleProjectConfigRejectsAMissingProject fails closed. Silently
// accepting an unknown project id would write config nothing ever reads, and
// report success for it.
func TestHandleProjectConfigRejectsAMissingProject(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	if _, err := sup.HandleProjectConfigGet(ctx, ipc.ProjectConfigGetArgs{}); err == nil {
		t.Error("config-get accepted an empty project id")
	}
	if err := sup.HandleProjectConfigApply(ctx, ipc.ProjectConfigApplyArgs{
		Upserts: map[string]string{"k": `"v"`},
	}); err == nil {
		t.Error("config-apply accepted an empty project id")
	}
}
