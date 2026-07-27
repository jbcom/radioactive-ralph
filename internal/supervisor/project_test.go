package supervisor

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// TestHandleProjectEnsureCreatesThenResolves pins the resolve-or-create
// contract: the first call registers the project, the second finds the same one
// rather than creating a duplicate.
func TestHandleProjectEnsureCreatesThenResolves(t *testing.T) {
	ctx := context.Background()
	s := &Supervisor{store: openTestStore(t)}
	args := ipc.ProjectEnsureArgs{
		Fingerprints: []ipc.ProjectFingerprint{{Kind: "abs_path", Value: "/tmp/demo"}},
		DisplayName:  "demo",
	}

	first, err := s.HandleProjectEnsure(ctx, args)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if !first.Created {
		t.Error("first ensure did not report Created")
	}
	if first.ProjectID == "" {
		t.Fatal("first ensure returned an empty project id")
	}

	second, err := s.HandleProjectEnsure(ctx, args)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if second.Created {
		t.Error("second ensure created a duplicate project")
	}
	if second.ProjectID != first.ProjectID {
		t.Fatalf("second ensure resolved %q, want %q", second.ProjectID, first.ProjectID)
	}
}

// TestHandleProjectEnsureAccumulatesNewFingerprints covers the case a directory
// gains a signal later — a git remote added after first use. A subsequent
// resolve by that new signal alone must find the same project, which only works
// if the ensure path accumulates identifiers rather than just touching.
func TestHandleProjectEnsureAccumulatesNewFingerprints(t *testing.T) {
	ctx := context.Background()
	s := &Supervisor{store: openTestStore(t)}

	first, err := s.HandleProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		Fingerprints: []ipc.ProjectFingerprint{{Kind: "abs_path", Value: "/tmp/demo"}},
		DisplayName:  "demo",
	})
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	// Same directory, now also reporting a git remote.
	if _, err := s.HandleProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		Fingerprints: []ipc.ProjectFingerprint{
			{Kind: "abs_path", Value: "/tmp/demo"},
			{Kind: "git_remote", Value: "git@example.com:demo.git"},
		},
		DisplayName: "demo",
	}); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	// The remote alone must now resolve to the same project.
	byRemote, err := s.HandleProjectEnsure(ctx, ipc.ProjectEnsureArgs{
		Fingerprints: []ipc.ProjectFingerprint{
			{Kind: "git_remote", Value: "git@example.com:demo.git"},
		},
	})
	if err != nil {
		t.Fatalf("resolve by remote: %v", err)
	}
	if byRemote.Created {
		t.Error("resolving by an accumulated fingerprint created a new project")
	}
	if byRemote.ProjectID != first.ProjectID {
		t.Fatalf("resolved %q, want the original %q", byRemote.ProjectID, first.ProjectID)
	}
}

func TestHandleProjectEnsureRejectsBadArgs(t *testing.T) {
	ctx := context.Background()
	s := &Supervisor{store: openTestStore(t)}

	for _, tc := range []struct {
		name string
		args ipc.ProjectEnsureArgs
	}{
		{"no fingerprints", ipc.ProjectEnsureArgs{DisplayName: "d"}},
		{"empty kind", ipc.ProjectEnsureArgs{
			Fingerprints: []ipc.ProjectFingerprint{{Value: "/tmp/x"}}, DisplayName: "d",
		}},
		{"empty value", ipc.ProjectEnsureArgs{
			Fingerprints: []ipc.ProjectFingerprint{{Kind: "abs_path"}}, DisplayName: "d",
		}},
		{
			// Creating needs a name; resolving does not. Refusing here beats
			// inventing one and leaving an unnamed project behind.
			name: "create without a display name",
			args: ipc.ProjectEnsureArgs{
				Fingerprints: []ipc.ProjectFingerprint{{Kind: "abs_path", Value: "/tmp/unnamed"}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.HandleProjectEnsure(ctx, tc.args); err == nil {
				t.Fatalf("accepted invalid args: %+v", tc.args)
			}
		})
	}
}
