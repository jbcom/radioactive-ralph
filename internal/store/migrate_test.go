package store

import (
	"database/sql"
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
