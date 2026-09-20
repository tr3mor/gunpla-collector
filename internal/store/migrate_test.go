package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}

// A brand-new database should run every migration file.
func TestMigrate_FreshDBReachesLatestVersion(t *testing.T) {
	s := openTestStore(t)

	names, err := migrationNames()
	if err != nil {
		t.Fatalf("migrationNames: %v", err)
	}
	if got := userVersion(t, s.db); got != len(names) {
		t.Fatalf("user_version = %d, want %d (one per migration file)", got, len(names))
	}
}

// Re-opening an already-migrated database must leave user_version unchanged.
func TestMigrate_ReopenIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open (first): %v", err)
	}
	before := userVersion(t, s1.db)
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open (second): %v", err)
	}
	defer s2.Close()
	after := userVersion(t, s2.db)

	if before != after {
		t.Fatalf("user_version changed on reopen: %d -> %d", before, after)
	}
}

// Simulates a database from before migrations existed: tables already
// present, user_version still at SQLite's default of 0. migrate must run
// migration 1 as a no-op and apply everything after it.
func TestMigrate_AppliesOnTopOfPreMigrationDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	raw, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	baseline, err := migrationsFS.ReadFile("migrations/0001_baseline.sql")
	if err != nil {
		t.Fatalf("read baseline migration: %v", err)
	}
	if _, err := raw.Exec(string(baseline)); err != nil {
		t.Fatalf("apply baseline schema directly: %v", err)
	}
	if got := userVersion(t, raw); got != 0 {
		t.Fatalf("user_version = %d before migrate, want 0", got)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	names, err := migrationNames()
	if err != nil {
		t.Fatalf("migrationNames: %v", err)
	}
	if got := userVersion(t, s.db); got != len(names) {
		t.Fatalf("user_version = %d after migrate, want %d", got, len(names))
	}
}
