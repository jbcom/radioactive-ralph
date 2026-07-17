package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonboulle/clockwork"
)

func TestBackupCreatesRestorableSnapshot(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "backup-project")
	planID := mustCreatePlan(t, s, projectID, "backup-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	destDir := t.TempDir()
	backupPath, err := s.Backup(ctx, destDir)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if filepath.Dir(backupPath) != destDir {
		t.Errorf("backup path %q not under destDir %q", backupPath, destDir)
	}

	// The backup must be a fully independent, openable SQLite DB with the
	// data present at snapshot time.
	restored, err := Open(ctx, Options{DSN: DSN(backupPath)})
	if err != nil {
		t.Fatalf("Open backup: %v", err)
	}
	defer func() { _ = restored.Close() }()

	got, err := restored.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask from backup: %v", err)
	}
	if got.Description != "first" {
		t.Errorf("restored task description = %q, want first", got.Description)
	}
}

func TestBackupRequiresDestDir(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.Backup(ctx, ""); err == nil {
		t.Error("Backup with empty destDir: want error, got nil")
	}
}

// TestBackupFailsIfDestinationAlreadyExists confirms Backup refuses to
// overwrite an existing file at its computed destination path — VACUUM
// INTO requires the destination not already exist, and Backup checks this
// itself up front with a clear error rather than surfacing SQLite's raw
// error.
func TestBackupFailsIfDestinationAlreadyExists(t *testing.T) {
	ctx := context.Background()
	s := openTestStoreWithClock(t, clockwork.NewFakeClockAt(mustParseTime(t, "2026-07-16T00:00:00Z")))
	destDir := t.TempDir()

	// First backup succeeds and creates the timestamped file.
	first, err := s.Backup(ctx, destDir)
	if err != nil {
		t.Fatalf("first Backup: %v", err)
	}

	// A second backup at the SAME clock instant computes the identical
	// destination filename (backup-<timestamp>.db) and must fail rather
	// than silently overwrite the first snapshot.
	if _, err := s.Backup(ctx, destDir); err == nil {
		t.Errorf("second Backup at the same clock instant (dest %s already exists): want error, got nil", first)
	}
}

// TestSQLStringLiteralEscapesEmbeddedQuotes confirms a destDir containing
// a single quote is safely escaped for VACUUM INTO's string-literal-only
// argument form (which has no parameter-binding equivalent), by asserting
// the round trip actually works end-to-end.
func TestSQLStringLiteralEscapesEmbeddedQuotes(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	mustCreateProject(t, s, "quote-project")

	root := t.TempDir()
	destDir := filepath.Join(root, "dir'with'quotes")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	backupPath, err := s.Backup(ctx, destDir)
	if err != nil {
		t.Fatalf("Backup into a quote-containing destDir: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
}
