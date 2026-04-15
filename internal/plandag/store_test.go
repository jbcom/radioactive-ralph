package plandag

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jonboulle/clockwork"
)

// TestOpenRunsMigrations confirms the embedded schema applies
// cleanly into a fresh SQLite DB and that user_version lands at
// currentSchemaVersion.
func TestOpenRunsMigrations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "plans.db")

	s, err := Open(ctx, Options{
		DSN:   "file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)",
		Clock: clockwork.NewFakeClockAt(mustParseTime("2026-04-15T00:00:00Z")),
		UUID: func() string {
			return "01945000-0000-7000-8000-000000000001"
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

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
		"plans", "plan_aliases", "intents", "analyses", "tasks",
		"task_deps", "parallelism_hints", "task_events",
		"sessions", "session_plans", "session_variants", "task_heartbeats",
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

// TestReopenIsIdempotent verifies that Open on an already-migrated
// DB is a no-op (doesn't re-run migrations or corrupt state).
func TestReopenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "plans.db")
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"

	s1, err := Open(ctx, Options{DSN: dsn})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// Insert a plan so we can verify data survives reopen.
	_, err = s1.DB().ExecContext(ctx,
		`INSERT INTO plans(id, slug, title, status) VALUES(?,?,?,?)`,
		"01945000-0000-7000-8000-000000000042",
		"test-plan",
		"Test",
		"active",
	)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	s1.Close()

	s2, err := Open(ctx, Options{DSN: dsn})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	var count int
	if err := s2.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plans WHERE slug = ?", "test-plan").Scan(&count); err != nil {
		t.Fatalf("select: %v", err)
	}
	if count != 1 {
		t.Errorf("plan count = %d, want 1", count)
	}
}

// TestRefuseNewerSchema confirms we reject a DB marked with a
// user_version higher than the current binary's currentSchemaVersion.
// Guards against running older ralph against state written by newer.
func TestRefuseNewerSchema(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "plans.db")
	dsn := "file:" + dbPath

	// Open once to create the file, bump user_version beyond our max,
	// then reopen — expect error.
	s, err := Open(ctx, Options{DSN: dsn})
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// Force user_version to a future value.
	if _, err := s.DB().ExecContext(ctx, "PRAGMA user_version = 999"); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	s.Close()

	_, err = Open(ctx, Options{DSN: dsn})
	if err == nil {
		t.Fatal("expected Open to refuse newer schema; got nil error")
	}
}

// TestForeignKeyCascade confirms plan deletion nukes dependent rows.
// Tests the ON DELETE CASCADE wiring in the schema.
func TestForeignKeyCascade(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	planID := "01945000-0000-7000-8000-00000000aaaa"
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO plans(id, slug, title, status) VALUES(?,?,?,?)`,
		planID, "cascade-test", "Cascade", "active")
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO tasks(id, plan_id, description, status) VALUES(?,?,?,?)`,
		"t1", planID, "first task", "pending")
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	_, err = s.DB().ExecContext(ctx,
		`INSERT INTO intents(plan_id, raw_input) VALUES(?,?)`,
		planID, "some intent")
	if err != nil {
		t.Fatalf("insert intent: %v", err)
	}

	// Delete plan; cascade should nuke tasks + intents.
	if _, err := s.DB().ExecContext(ctx, `DELETE FROM plans WHERE id = ?`, planID); err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	var taskCount, intentCount int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE plan_id = ?", planID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM intents WHERE plan_id = ?", planID).Scan(&intentCount); err != nil {
		t.Fatalf("count intents: %v", err)
	}
	if taskCount != 0 || intentCount != 0 {
		t.Errorf("cascade failed: tasks=%d intents=%d, want 0/0", taskCount, intentCount)
	}
}
