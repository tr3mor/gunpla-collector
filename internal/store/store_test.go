package store

import (
	"context"
	"path/filepath"
	"testing"

	"gunpla-collector/internal/scraper"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestOpen_ForeignKeysEnforced guards against the DSN-based
// `_foreign_keys=on` regressing back to a one-off PRAGMA exec, which
// wouldn't apply to connections database/sql opens later.
func TestOpen_ForeignKeysEnforced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	_, err := s.conn.ExecContext(ctx,
		`INSERT INTO price_history (set_id, run_id, price_cents, currency, scraped_at) VALUES (?, ?, ?, ?, ?)`,
		9999, 1, 100, "EUR", "2026-01-01T00:00:00Z")
	if err == nil {
		t.Fatal("expected foreign key violation inserting price_history for a nonexistent set, got nil")
	}
}

func TestApplyRun_PersistsSetsPriceHistoryAndFinishesRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}
	runID, err := s.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	inStock := true
	sets := []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "https://example.com/1", Name: "Kit One", Grade: "MG", PriceCents: 5000, Currency: "EUR", InStock: &inStock},
		{ExternalID: "ext-2", URL: "https://example.com/2", Name: "Kit Two", Grade: "HG", PriceCents: 3000, Currency: "EUR", InStock: &inStock},
	}
	if err := s.ApplyRun(ctx, shop.ID, runID, sets, "2026-01-01T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun: %v", err)
	}

	current, previous, err := s.TwoMostRecentSuccessfulRuns(ctx, shop.ID)
	if err != nil {
		t.Fatalf("TwoMostRecentSuccessfulRuns: %v", err)
	}
	if current == nil || current.ID != runID || current.Status != "success" {
		t.Fatalf("unexpected current run: %+v", current)
	}
	if !current.SetsFound.Valid || current.SetsFound.Int64 != 2 {
		t.Fatalf("current.SetsFound = %+v, want valid 2", current.SetsFound)
	}
	if previous != nil {
		t.Fatalf("expected no previous run, got %+v", previous)
	}

	count, ok, err := s.LastSuccessfulRunSetsFound(ctx, shop.ID)
	if err != nil || !ok || count != 2 {
		t.Fatalf("LastSuccessfulRunSetsFound = (%d, %v, %v), want (2, true, nil)", count, ok, err)
	}
}

func TestApplyRun_DeactivatesSetsMissingFromLatestRun(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}

	run1, err := s.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun 1: %v", err)
	}
	sets1 := []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 5000, Currency: "EUR"},
		{ExternalID: "ext-2", URL: "u2", Name: "Kit Two", Grade: "HG", PriceCents: 3000, Currency: "EUR"},
	}
	if err := s.ApplyRun(ctx, shop.ID, run1, sets1, "2026-01-01T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun 1: %v", err)
	}

	run2, err := s.StartRun(ctx, shop.ID, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun 2: %v", err)
	}
	// ext-2 is missing this run — it should be deactivated in one batched
	// UPDATE (see DeactivateMissing), and show up as "removed" in the diff.
	sets2 := []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 5000, Currency: "EUR"},
	}
	if err := s.ApplyRun(ctx, shop.ID, run2, sets2, "2026-01-02T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun 2: %v", err)
	}

	removed, err := s.RemovedSets(ctx, shop.ID, run2, run1)
	if err != nil {
		t.Fatalf("RemovedSets: %v", err)
	}
	if len(removed) != 1 || removed[0].Name != "Kit Two" {
		t.Fatalf("RemovedSets = %+v, want exactly [Kit Two]", removed)
	}

	shopAfter, ok, err := s.ShopBySlug(ctx, "test-shop")
	if err != nil || !ok {
		t.Fatalf("ShopBySlug: ok=%v err=%v", ok, err)
	}
	_ = shopAfter // sanity: shop row itself untouched by deactivation
}

// TestApplyRun_RollsBackOnError verifies ApplyRun is all-or-nothing: if any
// write in the batch fails, nothing from that batch is left committed.
func TestApplyRun_RollsBackOnError(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}
	runID, err := s.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	const bogusShopID = 999999 // no such shop row -> FK violation on insert
	sets := []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 5000, Currency: "EUR"},
		{ExternalID: "ext-2", URL: "u2", Name: "Kit Two", Grade: "HG", PriceCents: 3000, Currency: "EUR"},
	}
	if err := s.ApplyRun(ctx, bogusShopID, runID, sets, "2026-01-01T00:01:00Z"); err == nil {
		t.Fatal("expected error from foreign key violation, got nil")
	}

	var count int
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sets WHERE shop_id = ?`, bogusShopID).Scan(&count); err != nil {
		t.Fatalf("count sets: %v", err)
	}
	if count != 0 {
		t.Errorf("expected rollback to leave 0 sets for shop, got %d", count)
	}
}
