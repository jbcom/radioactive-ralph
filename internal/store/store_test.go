package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
)

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse time %q: %v", raw, err)
	}
	return tm
}

// TestOpenRunsMigrations confirms the embedded schema applies cleanly into
// a fresh SQLite DB and that user_version lands at currentSchemaVersion.
func TestOpenRunsMigrations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.db")

	s, err := Open(ctx, Options{
		DSN:   DSN(dbPath),
		Clock: clockwork.NewFakeClockAt(mustParseTime(t, "2026-04-15T00:00:00Z")),
		UUID: func() string {
			return "01945000-0000-7000-8000-000000000001"
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	var version int
	if err := s.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("user_version = %d, want %d", version, currentSchemaVersion)
	}

	// Confirm every expected table is present. Not exhaustive — just
	// enough to catch a typo'd CREATE TABLE.
	wanted := []string{
		"projects", "project_identifiers", "project_config",
		"plans", "tasks", "task_deps", "events",
		"sessions", "workers", "spend", "a2a_messages",
	}
	for _, name := range wanted {
		var seen string
		err := s.DB().QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			name).Scan(&seen)
		if err != nil {
			t.Errorf("missing table %q: %v", name, err)
		}
	}
}

// TestReopenIsIdempotent verifies that Open on an already-migrated DB is a
// no-op (doesn't re-run migrations or corrupt state).
func TestReopenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.db")
	dsn := DSN(dbPath)

	s1, err := Open(ctx, Options{DSN: dsn})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	projectID, err := s1.CreateProject(ctx, "test project", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/tmp/does-not-matter"},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, err = s1.DB().ExecContext(ctx,
		`INSERT INTO plans(id, project_id, slug, title, status) VALUES(?,?,?,?,?)`,
		"01945000-0000-7000-8000-000000000042",
		projectID,
		"test-plan",
		"Test",
		"active",
	)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	_ = s1.Close()

	s2, err := Open(ctx, Options{DSN: dsn})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	var count int
	if err := s2.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plans WHERE slug = ?", "test-plan").Scan(&count); err != nil {
		t.Fatalf("select: %v", err)
	}
	if count != 1 {
		t.Errorf("plan count = %d, want 1", count)
	}
}

// TestRefuseNewerSchema confirms we reject a DB marked with a user_version
// higher than the current binary's currentSchemaVersion. Guards against
// running an older ralph against state written by a newer one.
func TestRefuseNewerSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.db")
	dsn := DSN(dbPath)

	// Open once to create the file, bump user_version beyond our max,
	// then reopen — expect error.
	s, err := Open(ctx, Options{DSN: dsn})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, "PRAGMA user_version = 999"); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	_ = s.Close()

	_, err = Open(ctx, Options{DSN: dsn})
	if err == nil {
		t.Fatal("expected Open to refuse newer schema; got nil error")
	}
}

// TestForeignKeyCascade confirms plan deletion nukes dependent rows.
// Tests the ON DELETE CASCADE wiring in the schema.
func TestForeignKeyCascade(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: DSN(filepath.Join(t.TempDir(), "store.db"))})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	projectID, err := s.CreateProject(ctx, "cascade project", []Fingerprint{
		{Kind: FingerprintKindAbsPath, Value: "/tmp/cascade"},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	planID := "01945000-0000-7000-8000-00000000aaaa"
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO plans(id, project_id, slug, title, status) VALUES(?,?,?,?,?)`,
		planID, projectID, "cascade-test", "Cascade", "active")
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO tasks(id, plan_id, description, status) VALUES(?,?,?,?)`,
		"t1", planID, "first task", "pending")
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// Delete plan; cascade should nuke tasks.
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, planID); err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	var taskCount int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE plan_id = ?", planID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Errorf("cascade failed: tasks=%d, want 0", taskCount)
	}

	// project_identifiers should cascade on project delete too.
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var idCount int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM project_identifiers WHERE project_id = ?", projectID).Scan(&idCount); err != nil {
		t.Fatalf("count identifiers: %v", err)
	}
	if idCount != 0 {
		t.Errorf("cascade failed: project_identifiers=%d, want 0", idCount)
	}
}

// TestDSNEscaping confirms special characters in a path are percent-encoded
// so they survive the file: URI parse without being misread as URI syntax.
func TestDSNEscaping(t *testing.T) {
	dsn := DSN("/tmp/weird%dir/plans.db")
	if want := "file:/tmp/weird%25dir/plans.db?"; len(dsn) < len(want) || dsn[:len(want)] != want {
		t.Errorf("DSN did not escape '%%': got %q", dsn)
	}
}
