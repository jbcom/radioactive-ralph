package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
)

func TestListMigrationsOrdersByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.up.sql": &fstest.MapFile{Data: []byte("-- second")},
		"0001_first.up.sql":  &fstest.MapFile{Data: []byte("-- first")},
		"0010_tenth.up.sql":  &fstest.MapFile{Data: []byte("-- tenth")},
	}
	got, err := listMigrations(fsys, ".up.sql")
	if err != nil {
		t.Fatalf("listMigrations: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	wantOrder := []int{1, 2, 10}
	for i, m := range got {
		if m.version != wantOrder[i] {
			t.Errorf("got[%d].version = %d, want %d", i, m.version, wantOrder[i])
		}
	}
}

func TestListMigrationsSkipsMalformedNames(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_valid.up.sql":         &fstest.MapFile{Data: []byte("-- valid")},
		"noUnderscore.up.sql":       &fstest.MapFile{Data: []byte("-- no version prefix")},
		"_leadingunderscore.up.sql": &fstest.MapFile{Data: []byte("-- empty version")},
		"notaversion_x.up.sql":      &fstest.MapFile{Data: []byte("-- non-numeric version")},
		"0002_wrongsuffix.down.sql": &fstest.MapFile{Data: []byte("-- wrong suffix")},
	}
	got, err := listMigrations(fsys, ".up.sql")
	if err != nil {
		t.Fatalf("listMigrations: %v", err)
	}
	if len(got) != 1 || got[0].name != "0001_valid.up.sql" {
		t.Fatalf("got = %+v, want exactly [0001_valid.up.sql]", got)
	}
}

func TestListMigrationsSkipsDirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"0001_valid.up.sql":       &fstest.MapFile{Data: []byte("-- valid")},
		"0002_adir.up.sql/nested": &fstest.MapFile{Data: []byte("nested file under a dir-like name")},
	}
	got, err := listMigrations(fsys, ".up.sql")
	if err != nil {
		t.Fatalf("listMigrations: %v", err)
	}
	if len(got) != 1 || got[0].name != "0001_valid.up.sql" {
		t.Fatalf("got = %+v, want exactly [0001_valid.up.sql] (directory entries skipped)", got)
	}
}

func openRawSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", DSN(t.TempDir()+"/raw.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestApplyMigrationExecFailureDoesNotBumpVersion(t *testing.T) {
	db := openRawSQLite(t)

	err := applyMigration(db, 1, "THIS IS NOT VALID SQL;")
	if err == nil {
		t.Fatal("applyMigration with invalid SQL: want error, got nil")
	}

	v, err := readUserVersion(db)
	if err != nil {
		t.Fatalf("readUserVersion: %v", err)
	}
	if v != 0 {
		t.Errorf("user_version = %d after failed migration, want 0 (rolled back)", v)
	}
}

func TestApplyMigrationSuccessBumpsVersion(t *testing.T) {
	db := openRawSQLite(t)

	if err := applyMigration(db, 7, "CREATE TABLE t_migrate_test(id INTEGER PRIMARY KEY);"); err != nil {
		t.Fatalf("applyMigration: %v", err)
	}

	v, err := readUserVersion(db)
	if err != nil {
		t.Fatalf("readUserVersion: %v", err)
	}
	if v != 7 {
		t.Errorf("user_version = %d, want 7", v)
	}
}

func TestApplyMigrationSupportedRejectsNewerSchemaInsideTransaction(t *testing.T) {
	db := openRawSQLite(t)
	if _, err := db.Exec("PRAGMA user_version = 4"); err != nil {
		t.Fatalf("seed newer user_version: %v", err)
	}

	err := applyMigrationSupported(
		db, 3, currentSchemaVersion,
		"CREATE TABLE must_not_be_created(id INTEGER PRIMARY KEY);",
	)
	if err == nil {
		t.Fatal("applyMigrationSupported: want newer-schema error, got nil")
	}
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'must_not_be_created'`,
	).Scan(&count); err != nil {
		t.Fatalf("inspect rejected migration: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected migration created %d tables, want 0", count)
	}
}

func TestReadUserVersionDefaultsToZero(t *testing.T) {
	db := openRawSQLite(t)
	v, err := readUserVersion(db)
	if err != nil {
		t.Fatalf("readUserVersion: %v", err)
	}
	if v != 0 {
		t.Errorf("readUserVersion on a fresh DB = %d, want 0", v)
	}
}

// TestConcurrentOpenersSerializeMigrations is the multi-process analogue
// exercised with independent database/sql handles: every opener can observe a
// fresh database, but the immediate transaction's in-lock user_version recheck
// ensures each CREATE runs only once.
func TestConcurrentOpenersSerializeMigrations(t *testing.T) {
	ctx := context.Background()
	dsn := DSN(filepath.Join(t.TempDir(), "concurrent-open.db"))
	const openers = 12

	start := make(chan struct{})
	errs := make(chan error, openers)
	stores := make(chan *Store, openers)
	var ready sync.WaitGroup
	ready.Add(openers)

	for range openers {
		go func() {
			ready.Done()
			<-start
			store, err := Open(ctx, Options{DSN: dsn})
			if err == nil {
				stores <- store
			}
			errs <- err
		}()
	}
	ready.Wait()
	close(start)

	for range openers {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Open: %v", err)
		}
	}
	close(stores)
	for store := range stores {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open migrated DB: %v", err)
	}
	defer func() { _ = db.Close() }()
	version, err := readUserVersion(db)
	if err != nil {
		t.Fatalf("readUserVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", version, currentSchemaVersion)
	}
	for _, table := range []string{"projects", "a2a_messages", "task_metadata"} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
			table,
		).Scan(&count); err != nil {
			t.Fatalf("inspect table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s count = %d, want 1", table, count)
		}
	}
}
