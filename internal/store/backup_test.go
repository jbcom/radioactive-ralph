package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
