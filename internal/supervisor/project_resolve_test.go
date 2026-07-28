package supervisor

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestProjectEnsureResolveOnlyDoesNotCreate is what the desktop-launch path
// needs and why plain project-ensure could not serve it.
//
// A Finder/Explorer launch has an ARBITRARY working directory — usually not a
// repo, sometimes "/" — so the create-on-miss behavior would register whatever
// directory the file manager happened to hand us as a real project. That is a
// durable, operator-visible garbage row created by double-clicking an icon.
//
// ResolveOnly answers "is this a project I already know?" without writing.
func TestProjectEnsureResolveOnlyDoesNotCreate(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()

	reply, err := sup.HandleProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		ResolveOnly: true,
		Fingerprints: []ipc.ProjectFingerprint{
			{Kind: string(store.FingerprintKindAbsPath), Value: "/definitely/not/a/project"},
		},
	})
	if err != nil {
		t.Fatalf("HandleProjectEnsure(ResolveOnly): %v", err)
	}
	if reply.ProjectID != "" {
		t.Fatalf("ProjectID = %q, want empty — an unknown directory must not resolve", reply.ProjectID)
	}
	if reply.Created {
		t.Fatal("ResolveOnly reported Created; it must never write")
	}

	// Proof of non-creation: resolving the SAME fingerprint again must still
	// find nothing. A stronger check than counting rows, since it asserts the
	// exact thing that would break — the directory becoming known.
	_, found, err := sup.store.ResolveProject(ctx, []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/definitely/not/a/project"},
	})
	if err != nil {
		t.Fatalf("ResolveProject: %v", err)
	}
	if found {
		t.Fatal("ResolveOnly registered the directory; double-clicking an icon in " +
			"an arbitrary directory must not create a project")
	}
}

// TestProjectEnsureResolveOnlyFindsAKnownProject is the other half: when the
// launch directory IS a known project, the GUI must scope to it.
func TestProjectEnsureResolveOnlyFindsAKnownProject(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	dir := t.TempDir()
	want, err := sup.store.CreateProject(ctx, "known",
		[]store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: dir}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	reply, err := sup.HandleProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		ResolveOnly:  true,
		Fingerprints: []ipc.ProjectFingerprint{{Kind: string(store.FingerprintKindAbsPath), Value: dir}},
	})
	if err != nil {
		t.Fatalf("HandleProjectEnsure(ResolveOnly): %v", err)
	}
	if reply.ProjectID != want {
		t.Fatalf("ProjectID = %q, want %q", reply.ProjectID, want)
	}
	if reply.Created {
		t.Error("Created = true on a resolve of an existing project")
	}
}

// TestProjectEnsureStillCreatesByDefault guards the init path: making
// ResolveOnly available must not change what plain project-ensure does.
func TestProjectEnsureStillCreatesByDefault(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()
	dir := t.TempDir()

	reply, err := sup.HandleProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		Fingerprints: []ipc.ProjectFingerprint{{Kind: string(store.FingerprintKindAbsPath), Value: dir}},
		DisplayName:  "new",
	})
	if err != nil {
		t.Fatalf("HandleProjectEnsure: %v", err)
	}
	if reply.ProjectID == "" || !reply.Created {
		t.Fatalf("reply = %+v, want a created project", reply)
	}
}
