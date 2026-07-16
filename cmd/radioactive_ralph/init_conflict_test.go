package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

// writeProjectConfigFile writes a minimal TOML project-config-file with a
// single top-level "model" key, for exercising the --init conflict UX.
func writeProjectConfigFile(t *testing.T, dir, model string) string {
	t.Helper()
	path := filepath.Join(dir, "project.toml")
	content := "model = \"" + model + "\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write project config file: %v", err)
	}
	return path
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// TestInitMode_ProjectConfigConflictDefaultsToAutoRemove proves the chosen
// --init conflict UX (task 4 of the Phase 6c tech-debt pass): a second
// --init --project-config-file whose "model" key conflicts with an
// already-stored value is, by default (no --force-override), auto-removed
// from what gets applied — the run succeeds, the stored value is left
// untouched, and a clear notice names the skipped key.
func TestInitMode_ProjectConfigConflictDefaultsToAutoRemove(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	// First --init establishes "model" = "stored-model" as the baseline.
	firstConfig := writeProjectConfigFile(t, t.TempDir(), "stored-model")
	cmd1 := newRootCmd(context.Background())
	cmd1.SetArgs([]string{"--init", "--project-config-file", firstConfig})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first --init: %v", err)
	}

	// Second --init tries to change "model" to a conflicting value, with
	// no --force-override: the conflicting key must be skipped.
	secondConfig := writeProjectConfigFile(t, t.TempDir(), "incoming-model")
	cmd2 := newRootCmd(context.Background())
	cmd2.SetArgs([]string{"--init", "--project-config-file", secondConfig})
	out := captureStdout(t, func() {
		if err := cmd2.Execute(); err != nil {
			t.Fatalf("second --init: %v", err)
		}
	})

	if !strings.Contains(out, "SKIPPED") || !strings.Contains(out, "model") {
		t.Errorf("stdout = %q, want a notice naming the skipped \"model\" key", out)
	}

	assertStoredModel(t, stateDir, "stored-model")
}

// TestInitMode_ProjectConfigConflictForceOverrideApplies proves the
// --force-override escape hatch actually applies the conflicting value
// (verbatim, no auto-remove) instead of skipping it.
func TestInitMode_ProjectConfigConflictForceOverrideApplies(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	firstConfig := writeProjectConfigFile(t, t.TempDir(), "stored-model")
	cmd1 := newRootCmd(context.Background())
	cmd1.SetArgs([]string{"--init", "--project-config-file", firstConfig})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("first --init: %v", err)
	}

	secondConfig := writeProjectConfigFile(t, t.TempDir(), "incoming-model")
	cmd2 := newRootCmd(context.Background())
	cmd2.SetArgs([]string{"--init", "--project-config-file", secondConfig, "--force-override"})
	out := captureStdout(t, func() {
		if err := cmd2.Execute(); err != nil {
			t.Fatalf("second --init --force-override: %v", err)
		}
	})

	if !strings.Contains(out, "force-override") {
		t.Errorf("stdout = %q, want a notice mentioning force-override applying the conflict", out)
	}

	assertStoredModel(t, stateDir, "incoming-model")
}

// assertStoredModel resolves the current directory's project id (the
// caller must already have chdir'd into the project directory) and asserts
// its stored "model" project-config value equals want.
func assertStoredModel(t *testing.T, stateDir, want string) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	resolvedCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	fps, err := store.Fingerprints(ctx, resolvedCwd)
	if err != nil {
		t.Fatalf("Fingerprints: %v", err)
	}
	projectID, found, err := st.ResolveProject(ctx, fps)
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
	got := strings.Trim(cfg["model"], `"`)
	if got != want {
		t.Errorf("stored model = %q, want %q", got, want)
	}
}
