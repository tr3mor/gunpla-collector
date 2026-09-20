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

// TestMigrate_FreshDBReachesLatestVersion opens a brand-new database and
// asserts every migration file ran, leaving user_version at the count of
// migration files.
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

// TestMigrate_ReopenIsNoop verifies re-opening an already-migrated database
// doesn't error and leaves user_version unchanged (every migration is
// skipped because its number is <= the stored version).
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

// TestMigrate_AppliesOnTopOfPreMigrationDatabase simulates a database
// created before migrations existed: tables already present (via the
// baseline schema applied directly, bypassing migrate), user_version left
// at its SQLite default of 0. migrate must run migration 1 as a no-op
// (CREATE TABLE IF NOT EXISTS) and apply every later migration on top.
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
