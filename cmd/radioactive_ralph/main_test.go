package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

// chdir temporarily changes the working directory for the duration of a
// test, restoring it on cleanup. Several of dispatchRoot's paths resolve
// the project against os.Getwd().
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestInitMode_CreatesProject(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"--init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--init: %v", err)
	}

	// A second --init on the same directory must be idempotent (re-resolve
	// the existing project rather than erroring or duplicating it).
	cmd2 := newRootCmd(context.Background())
	cmd2.SetArgs([]string{"--init"})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("second --init: %v", err)
	}

	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Recompute fingerprints from the actually-resolved cwd, not the raw
	// t.TempDir() string: on macOS /tmp is a symlink to /private/tmp, and
	// os.Getwd() (which runInitMode uses) returns the resolved form, so
	// asserting against the pre-chdir path would look for the wrong value.
	resolvedCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	fps, err := store.Fingerprints(context.Background(), resolvedCwd)
	if err != nil {
		t.Fatalf("Fingerprints: %v", err)
	}
	var count int
	for _, fp := range fps {
		if fp.Kind != store.FingerprintKindAbsPath {
			continue
		}
		row := st.DB().QueryRow(`SELECT COUNT(*) FROM project_identifiers WHERE kind = ? AND value = ?`, fp.Kind, fp.Value)
		if err := row.Scan(&count); err != nil {
			t.Fatalf("count identifiers: %v", err)
		}
	}
	if count != 1 {
		t.Errorf("abs_path identifier rows = %d, want exactly 1 (init must be idempotent)", count)
	}
}

func TestClientMode_NoSupervisorFailsClearly(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when no supervisor is running, got nil")
	}
	// Under `go test` stdin/stdout are NOT terminals, so the interactive
	// first-run wizard must NOT run — the exact print-commands-and-fail path
	// is preserved. Guard the invariant so a future wizard change can't
	// silently start prompting in a non-interactive context.
	if onboardingInteractive() {
		t.Fatal("onboardingInteractive() = true under `go test`; the wizard must never run non-interactively")
	}
	if !errors.Is(err, errNoSupervisorListening) {
		t.Errorf("err = %v, want errNoSupervisorListening on the non-interactive path", err)
	}
}

func TestClientMode_FindsRunningSupervisor(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- supervisor.Run(ctx, supervisor.Options{RuntimeDir: stateDir, Store: st})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, findErr := supervisor.Find(stateDir)
		if findErr == nil {
			_ = c.Close()
			break
		}
		if !errors.Is(findErr, supervisor.ErrNoSupervisor) {
			t.Fatalf("unexpected Find error: %v", findErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cmd := newRootCmd(context.Background())
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("client mode with a live supervisor: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not exit within 3s of ctx cancel")
	}
}

func TestSupervisorDBPath(t *testing.T) {
	got := storeDBPath("/tmp/example")
	want := filepath.Join("/tmp/example", "ralph.db")
	if got != want {
		t.Errorf("storeDBPath() = %q, want %q", got, want)
	}
}
