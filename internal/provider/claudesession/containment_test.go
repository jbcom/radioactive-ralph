package claudesession

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// There is deliberately NO behavioral escape test here, unlike
// TestEveryDeclarativeShapeConfinesItsTurn in the parent package. One was
// written and removed: Spawn returns as soon as the process starts and Close
// immediately signals it, so a stand-in script is killed before its body runs —
// measured, with the script's own log coming back empty. The escape file was
// therefore absent whether or not containment was applied, so the test passed
// with the feature DISABLED. A test that cannot fail is worse than no test,
// because it reads as coverage.
//
// The fail-closed test below is the one that actually discriminates: disabling
// the containment branch reds it. Real escape behavior is covered where the
// process is driven to completion — the declarative runner's shapes.

// TestSpawnFailsClosedOnAnUnusableContainmentRoot pins the half that can be
// proven here: a root that cannot back a policy must abort the spawn, never
// degrade to an unconfined session. Silently dropping the confinement is what
// turns a security boundary into a false guarantee.
func TestSpawnFailsClosedOnAnUnusableContainmentRoot(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("containment is implemented for darwin and linux, not %s", runtime.GOOS)
	}
	// A regular FILE is not a usable root; contain.NewPolicy must reject it.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s, err := Spawn(context.Background(), Options{
		ClaudeBin:       "/bin/sh",
		WorkingDir:      t.TempDir(),
		ContainmentRoot: file,
		SessionID:       "22222222-2222-2222-2222-222222222222",
	})
	if s != nil {
		_ = s.Close()
	}
	if err == nil {
		t.Fatal("Spawn accepted an unusable containment root — it must fail closed " +
			"rather than return a session whose subprocess is unconfined")
	}
}
