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

// Migration 5 replaces price_history.in_stock with availability; existing
// rows must keep their meaning (and unknown stays unknown).
func TestMigrate_BackfillsAvailabilityFromInStock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	raw, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	baseline, err := migrationsFS.ReadFile("migrations/0001_baseline.sql")
	if err != nil {
		t.Fatalf("read baseline migration: %v", err)
	}
	stmts := []string{
		string(baseline),
		`INSERT INTO shops (id, slug, name, base_url) VALUES (1, 's', 'S', 'u')`,
		`INSERT INTO scrape_runs (id, shop_id, started_at, status) VALUES (1, 1, 't', 'success')`,
	}
	for i, in := range []string{"1", "0", "NULL"} {
		stmts = append(stmts,
			`INSERT INTO sets (id, shop_id, external_id, url, name, first_seen_at, last_seen_at) VALUES (`+string(rune('1'+i))+`, 1, 'e`+string(rune('1'+i))+`', 'u', 'n', 't', 't')`,
			`INSERT INTO price_history (set_id, run_id, price_cents, in_stock, scraped_at) VALUES (`+string(rune('1'+i))+`, 1, 100, `+in+`, 't')`)
	}
	for _, q := range stmts {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	want := map[int]string{1: "in_stock", 2: "out_of_stock", 3: ""}
	for setID, w := range want {
		var got string
		if err := s.db.QueryRow(`SELECT COALESCE(availability, '') FROM price_history WHERE set_id = ?`, setID).Scan(&got); err != nil {
			t.Fatalf("set %d: %v", setID, err)
		}
		if got != w {
			t.Errorf("set %d availability = %q, want %q", setID, got, w)
		}
	}
}
