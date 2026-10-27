package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/scraper"
)

// seedAtVersion builds a database as it was before migration `upTo+1`: the
// first upTo migrations applied, then the given statements run.
func seedAtVersion(t *testing.T, path string, upTo int, stmts ...string) {
	t.Helper()
	raw, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	names, err := migrationNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names[:upTo] {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := raw.Exec(`PRAGMA user_version = ` + strconv.Itoa(upTo)); err != nil {
		t.Fatal(err)
	}
	for _, q := range stmts {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

// Migration 10 drops the Gundam Store dollar history and the runs it
// belonged to, and nothing else.
func TestMigrate_DropsGundamStoreDollarHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seedAtVersion(t, path, 9,
		`INSERT INTO shops (id, slug, name, base_url) VALUES (1, 'gundamstore', 'Gundam Store', 'u'), (2, 'geeksheaven', 'GeeksHeaven', 'u')`,
		`INSERT INTO scrape_runs (id, shop_id, started_at, status) VALUES (1, 1, 't', 'success'), (2, 1, 't', 'success'), (3, 2, 't', 'success')`,
		`INSERT INTO sets (id, shop_id, external_id, url, name, ean, first_seen_at, last_seen_at) VALUES
			(1, 1, 'g1', 'u', 'GS kit', '4573102638236', 't', 't'),
			(2, 2, 'h1', 'u', 'GH kit', NULL, 't', 't')`,
		`INSERT INTO price_history (set_id, run_id, price_cents, currency, eur_cents, fx_rate, scraped_at) VALUES
			(1, 1, 8800, 'USD', 7850, 0.892, 't'),
			(1, 2, 8800, 'USD', 7850, 0.892, 't'),
			(2, 3, 5000, 'EUR', 5000, 1, 't')`,
	)

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	count := func(q string) int {
		t.Helper()
		var n int
		if err := s.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM price_history WHERE set_id = 1`); n != 0 {
		t.Errorf("Gundam Store price records left = %d, want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM scrape_runs WHERE shop_id = 1`); n != 0 {
		t.Errorf("Gundam Store runs left = %d, want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM price_history WHERE set_id = 2 AND eur_cents = 5000`); n != 1 {
		t.Errorf("GeeksHeaven price records = %d, want 1 unchanged", n)
	}
	if n := count(`SELECT COUNT(*) FROM scrape_runs WHERE shop_id = 2`); n != 1 {
		t.Errorf("GeeksHeaven runs = %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM sets WHERE id = 1 AND ean = '4573102638236'`); n != 1 {
		t.Errorf("Gundam Store listing with its barcode = %d, want kept", n)
	}
	if n := count(`SELECT COUNT(*) FROM sets`); n != 2 {
		t.Errorf("sets = %d, want 2", n)
	}
}

// Re-opening an upgraded database, and a database with no Gundam Store at
// all, are no-ops.
func TestMigrate_DropGundamStoreHistoryNoGundamStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seedAtVersion(t, path, 9,
		`INSERT INTO shops (id, slug, name, base_url) VALUES (1, 'geeksheaven', 'GeeksHeaven', 'u')`,
		`INSERT INTO scrape_runs (id, shop_id, started_at, status) VALUES (1, 1, 't', 'success')`,
		`INSERT INTO sets (id, shop_id, external_id, url, name, first_seen_at, last_seen_at) VALUES (1, 1, 'h1', 'u', 'n', 't', 't')`,
		`INSERT INTO price_history (set_id, run_id, price_cents, currency, eur_cents, fx_rate, scraped_at) VALUES (1, 1, 5000, 'EUR', 5000, 1, 't')`,
	)
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM price_history`).Scan(&n); err != nil || n != 1 {
			t.Errorf("open %d: price records = %d, %v; want 1", i+1, n, err)
		}
		s.Close()
	}
}

// After migration 10 the shop has no earlier run, so its next collect is a
// first run: nothing to compare the set count with (no mass-removal guard),
// and a report of it is a baseline (no previous reported run to diff
// against). Listings from before are still there to be matched up.
func TestMigrate_NextGundamStoreCollectIsFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seedAtVersion(t, path, 9,
		`INSERT INTO shops (id, slug, name, base_url) VALUES (1, 'gundamstore', 'Gundam Store', 'u')`,
		`INSERT INTO scrape_runs (id, shop_id, started_at, finished_at, status, sets_found, reported_at) VALUES (1, 1, 't', 't', 'success', 1000, 't')`,
		`INSERT INTO sets (id, shop_id, external_id, url, name, first_seen_at, last_seen_at) VALUES (1, 1, 'g1', 'u', 'GS kit', 't', 't')`,
		`INSERT INTO price_history (set_id, run_id, price_cents, currency, eur_cents, fx_rate, scraped_at) VALUES (1, 1, 8800, 'USD', 7850, 0.892, 't')`,
	)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if n, ok, err := s.LastSuccessfulRunSetsFound(ctx, 1); err != nil || ok {
		t.Fatalf("LastSuccessfulRunSetsFound = %d, %v, %v; want no previous run", n, ok, err)
	}

	run, err := s.StartRun(ctx, 1, "2026-10-27T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	// One listing where 1000 were found before: would trip the guard if the
	// old run were still there.
	err = s.ApplyRun(ctx, 1, run, []scraper.ScrapedSet{
		{ExternalID: "g1", URL: "u", Name: "GS kit", Grade: "MG", PriceCents: 69694, Currency: "DKK"},
	}, "2026-10-27T00:00:00Z", fx.Rates{"DKK": 0.134})
	if err != nil {
		t.Fatal(err)
	}

	current, previous, err := s.LatestUnreportedRun(ctx, 1)
	if err != nil || current == nil || current.ID != run || previous != nil {
		t.Errorf("LatestUnreportedRun = %+v, %+v, %v; want the new run and no previous", current, previous, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sets WHERE shop_id = 1`).Scan(&n); err != nil || n != 1 {
		t.Errorf("sets = %d, %v; want the existing listing reused", n, err)
	}
}
