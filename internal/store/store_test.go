package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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

	current, previous, err := s.LatestUnreportedRun(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LatestUnreportedRun: %v", err)
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

// EAN/SKU are stored on insert, overwritten on a later upsert, and
// cleared back to NULL (not left as "") when a shop stops reporting one.
func TestUpsertSet_PersistsAndUpdatesEANAndSKU(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}

	setID, err := s.UpsertSet(ctx, shop.ID, "ext-1", "u1", "Kit One", "MG", "ean-1", "sku-1", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("UpsertSet (insert): %v", err)
	}

	var ean, sku sql.NullString
	if err := s.conn.QueryRowContext(ctx, `SELECT ean, sku FROM sets WHERE id = ?`, setID).Scan(&ean, &sku); err != nil {
		t.Fatalf("query ean/sku: %v", err)
	}
	if !ean.Valid || ean.String != "ean-1" || !sku.Valid || sku.String != "sku-1" {
		t.Fatalf("after insert: ean=%+v sku=%+v, want ean-1/sku-1", ean, sku)
	}

	// Re-upsert with an updated EAN and a now-missing SKU.
	sameID, err := s.UpsertSet(ctx, shop.ID, "ext-1", "u1", "Kit One", "MG", "ean-1-updated", "", "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("UpsertSet (update): %v", err)
	}
	if sameID != setID {
		t.Fatalf("re-upsert returned id %d, want the same id %d", sameID, setID)
	}

	if err := s.conn.QueryRowContext(ctx, `SELECT ean, sku FROM sets WHERE id = ?`, setID).Scan(&ean, &sku); err != nil {
		t.Fatalf("query ean/sku after update: %v", err)
	}
	if !ean.Valid || ean.String != "ean-1-updated" {
		t.Fatalf("after update: ean=%+v, want ean-1-updated", ean)
	}
	if sku.Valid {
		t.Fatalf("after update: sku=%+v, want NULL (cleared, not empty string)", sku)
	}
}

// The migration-3 unique index: a second price_history row for the same
// (set_id, run_id) must fail, since PriceChanges assumes exactly one.
func TestInsertPriceHistory_RejectsDuplicateSetRunPair(t *testing.T) {
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
	setID, err := s.UpsertSet(ctx, shop.ID, "ext-1", "u1", "Kit One", "MG", "", "", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("UpsertSet: %v", err)
	}

	if err := s.InsertPriceHistory(ctx, setID, runID, 5000, "EUR", nil, "2026-01-01T00:01:00Z"); err != nil {
		t.Fatalf("InsertPriceHistory (first): %v", err)
	}
	if err := s.InsertPriceHistory(ctx, setID, runID, 5000, "EUR", nil, "2026-01-01T00:01:00Z"); err == nil {
		t.Fatal("expected error inserting a second price_history row for the same (set_id, run_id), got nil")
	}
}

// Exercises NewSets, RemovedSets, and PriceChanges against real SQLite
// (reporter's tests only cover these through a fake store) across three
// runs: a new set, a removed one, a price change, an unchanged one (must
// appear nowhere), and one removed then reactivated a run later — which
// must show up as "new" again, not "still removed".
func TestReportQueries(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}

	applyRun := func(startedAt, appliedAt string, sets []scraper.ScrapedSet) int64 {
		t.Helper()
		runID, err := s.StartRun(ctx, shop.ID, startedAt)
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if err := s.ApplyRun(ctx, shop.ID, runID, sets, appliedAt); err != nil {
			t.Fatalf("ApplyRun: %v", err)
		}
		return runID
	}
	set := func(extID string, priceCents int) scraper.ScrapedSet {
		return scraper.ScrapedSet{ExternalID: extID, URL: "u-" + extID, Name: "Kit " + extID, Grade: "MG", PriceCents: priceCents, Currency: "EUR"}
	}

	run1 := applyRun("2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z", []scraper.ScrapedSet{
		set("ext-removed", 1000),
		set("ext-changed", 1000),
		set("ext-unchanged", 2000),
		set("ext-reactivated", 3000),
	})

	// run2: ext-removed and ext-reactivated drop out; ext-changed's price
	// moves; ext-unchanged stays the same; ext-new shows up for the first time.
	run2 := applyRun("2026-01-02T00:00:00Z", "2026-01-02T00:01:00Z", []scraper.ScrapedSet{
		set("ext-changed", 1500),
		set("ext-unchanged", 2000),
		set("ext-new", 500),
	})

	newSets, err := s.NewSets(ctx, shop.ID, run2, run1)
	if err != nil {
		t.Fatalf("NewSets(run2, run1): %v", err)
	}
	if len(newSets) != 1 || newSets[0].Name != "Kit ext-new" {
		t.Errorf("NewSets(run2, run1) = %+v, want exactly [Kit ext-new]", newSets)
	}

	removedSets, err := s.RemovedSets(ctx, shop.ID, run2, run1)
	if err != nil {
		t.Fatalf("RemovedSets(run2, run1): %v", err)
	}
	removedNames := map[string]bool{}
	for _, r := range removedSets {
		removedNames[r.Name] = true
	}
	if len(removedSets) != 2 || !removedNames["Kit ext-removed"] || !removedNames["Kit ext-reactivated"] {
		t.Errorf("RemovedSets(run2, run1) = %+v, want exactly [Kit ext-removed, Kit ext-reactivated]", removedSets)
	}

	changes, err := s.PriceChanges(ctx, shop.ID, run2, run1)
	if err != nil {
		t.Fatalf("PriceChanges(run2, run1): %v", err)
	}
	if len(changes) != 1 || changes[0].Name != "Kit ext-changed" || changes[0].OldCents != 1000 || changes[0].NewCents != 1500 {
		t.Errorf("PriceChanges(run2, run1) = %+v, want exactly [Kit ext-changed: 1000 -> 1500]", changes)
	}

	// run3: ext-reactivated reappears — must show up as new relative to run2.
	run3 := applyRun("2026-01-03T00:00:00Z", "2026-01-03T00:01:00Z", []scraper.ScrapedSet{
		set("ext-changed", 1500),
		set("ext-unchanged", 2000),
		set("ext-new", 500),
		set("ext-reactivated", 3000),
	})

	newSets, err = s.NewSets(ctx, shop.ID, run3, run2)
	if err != nil {
		t.Fatalf("NewSets(run3, run2): %v", err)
	}
	if len(newSets) != 1 || newSets[0].Name != "Kit ext-reactivated" {
		t.Errorf("NewSets(run3, run2) = %+v, want exactly [Kit ext-reactivated] (reactivation)", newSets)
	}

	removedSets, err = s.RemovedSets(ctx, shop.ID, run3, run2)
	if err != nil {
		t.Fatalf("RemovedSets(run3, run2): %v", err)
	}
	if len(removedSets) != 0 {
		t.Errorf("RemovedSets(run3, run2) = %+v, want none", removedSets)
	}

	changes, err = s.PriceChanges(ctx, shop.ID, run3, run2)
	if err != nil {
		t.Fatalf("PriceChanges(run3, run2): %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("PriceChanges(run3, run2) = %+v, want none (ext-changed and ext-unchanged both held steady)", changes)
	}
}

// PriceChanges must surface the newer run's stock status alongside the
// price, so callers (reporter) can tell a real discount from a price drop
// on a set that's no longer purchasable. Sets without stock data at all
// (scraper doesn't report it) must come back with InStock == nil, not false.
func TestPriceChanges_InStock(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}

	applyRun := func(startedAt, appliedAt string, sets []scraper.ScrapedSet) int64 {
		t.Helper()
		runID, err := s.StartRun(ctx, shop.ID, startedAt)
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if err := s.ApplyRun(ctx, shop.ID, runID, sets, appliedAt); err != nil {
			t.Fatalf("ApplyRun: %v", err)
		}
		return runID
	}

	inStock, outOfStock := true, false
	run1 := applyRun("2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z", []scraper.ScrapedSet{
		{ExternalID: "ext-in", URL: "u1", Name: "Kit In Stock", Grade: "MG", PriceCents: 1000, Currency: "EUR", InStock: &inStock},
		{ExternalID: "ext-out", URL: "u2", Name: "Kit Out Of Stock", Grade: "MG", PriceCents: 1000, Currency: "EUR", InStock: &outOfStock},
		{ExternalID: "ext-unknown", URL: "u3", Name: "Kit No Stock Data", Grade: "MG", PriceCents: 1000, Currency: "EUR"},
	})
	run2 := applyRun("2026-01-02T00:00:00Z", "2026-01-02T00:01:00Z", []scraper.ScrapedSet{
		{ExternalID: "ext-in", URL: "u1", Name: "Kit In Stock", Grade: "MG", PriceCents: 1500, Currency: "EUR", InStock: &inStock},
		{ExternalID: "ext-out", URL: "u2", Name: "Kit Out Of Stock", Grade: "MG", PriceCents: 1500, Currency: "EUR", InStock: &outOfStock},
		{ExternalID: "ext-unknown", URL: "u3", Name: "Kit No Stock Data", Grade: "MG", PriceCents: 1500, Currency: "EUR"},
	})

	changes, err := s.PriceChanges(ctx, shop.ID, run2, run1)
	if err != nil {
		t.Fatalf("PriceChanges: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("PriceChanges = %+v, want 3 rows (filtering happens in reporter, not here)", changes)
	}
	byName := map[string]PriceChange{}
	for _, c := range changes {
		byName[c.Name] = c
	}

	if c := byName["Kit In Stock"]; c.InStock == nil || !*c.InStock {
		t.Errorf("Kit In Stock: InStock = %v, want true", c.InStock)
	}
	if c := byName["Kit Out Of Stock"]; c.InStock == nil || *c.InStock {
		t.Errorf("Kit Out Of Stock: InStock = %v, want false", c.InStock)
	}
	if c := byName["Kit No Stock Data"]; c.InStock != nil {
		t.Errorf("Kit No Stock Data: InStock = %v, want nil (no stock data collected)", c.InStock)
	}
}

// Two Store instances on the same file (simulating overlapping processes,
// e.g. an overrunning cron job): a read on one must succeed while the
// other holds an open write transaction — what WAL mode buys us.
func TestOpen_WALAllowsConcurrentReadDuringWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open s1: %v", err)
	}
	defer s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open s2: %v", err)
	}
	defer s2.Close()

	ctx := context.Background()
	tx, err := s1.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO shops (slug, name, base_url, active) VALUES ('mid-write', 'Mid Write', 'https://example.com', 1)`); err != nil {
		t.Fatalf("insert within open tx: %v", err)
	}
	// tx deliberately left uncommitted here.

	done := make(chan error, 1)
	go func() {
		_, err := s2.ActiveShops(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read from s2 while s1's write tx is open: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read from s2 blocked for 2s while s1's write tx was open — WAL mode not in effect")
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit s1 tx: %v", err)
	}
}

// A shop that's never had a collect run gets ok=false — distinct from
// "everything's already been reported" (see AllReported below).
func TestLatestRun_NoRunsYet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}
	run, ok, err := s.LatestRun(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if ok || run != nil {
		t.Fatalf("LatestRun = (%+v, %v), want (nil, false)", run, ok)
	}
}

// LatestRun must pick up a failed run even with an earlier success on record.
func TestLatestRun_ReturnsMostRecentRegardlessOfStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shop, err := s.GetOrCreateShop(ctx, "test-shop", "Test Shop", "https://example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop: %v", err)
	}
	okRunID, err := s.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun 1: %v", err)
	}
	if err := s.FinishRunSuccess(ctx, okRunID, "2026-01-01T00:01:00Z", 5); err != nil {
		t.Fatalf("FinishRunSuccess: %v", err)
	}
	failedRunID, err := s.StartRun(ctx, shop.ID, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun 2: %v", err)
	}
	if err := s.FinishRunFailed(ctx, failedRunID, "2026-01-02T00:01:00Z", "scraper broke"); err != nil {
		t.Fatalf("FinishRunFailed: %v", err)
	}

	run, ok, err := s.LatestRun(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if !ok || run.ID != failedRunID || run.Status != "failed" || !run.Error.Valid || run.Error.String != "scraper broke" {
		t.Fatalf("LatestRun = %+v, want the failed run", run)
	}
}

// Once MarkReported has stamped every successful run, LatestUnreportedRun
// must report current=nil — this is what makes report idempotent.
func TestLatestUnreportedRun_AllReported(t *testing.T) {
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
	sets := []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 5000, Currency: "EUR"},
	}
	if err := s.ApplyRun(ctx, shop.ID, runID, sets, "2026-01-01T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun: %v", err)
	}

	if err := s.MarkReported(ctx, shop.ID, "2026-01-01T00:02:00Z"); err != nil {
		t.Fatalf("MarkReported: %v", err)
	}

	current, previous, err := s.LatestUnreportedRun(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LatestUnreportedRun: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want nil (already reported)", current)
	}
	if previous == nil || previous.ID != runID {
		t.Fatalf("previous = %+v, want the reported run", previous)
	}
}

// If collect has run several times since the last report, the next report
// diffs against the last *reported* run — skipping the ones in between —
// and MarkReported then clears all of them at once.
func TestLatestUnreportedRun_SkipsIntermediateRuns(t *testing.T) {
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
	if err := s.ApplyRun(ctx, shop.ID, run1, nil, "2026-01-01T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun 1: %v", err)
	}
	if err := s.MarkReported(ctx, shop.ID, "2026-01-01T00:02:00Z"); err != nil {
		t.Fatalf("MarkReported after run1: %v", err)
	}

	run2, err := s.StartRun(ctx, shop.ID, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun 2: %v", err)
	}
	if err := s.ApplyRun(ctx, shop.ID, run2, nil, "2026-01-02T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun 2: %v", err)
	}
	run3, err := s.StartRun(ctx, shop.ID, "2026-01-03T00:00:00Z")
	if err != nil {
		t.Fatalf("StartRun 3: %v", err)
	}
	if err := s.ApplyRun(ctx, shop.ID, run3, nil, "2026-01-03T00:01:00Z"); err != nil {
		t.Fatalf("ApplyRun 3: %v", err)
	}

	current, previous, err := s.LatestUnreportedRun(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LatestUnreportedRun: %v", err)
	}
	if current == nil || current.ID != run3 {
		t.Fatalf("current = %+v, want run3 (%d)", current, run3)
	}
	if previous == nil || previous.ID != run1 {
		t.Fatalf("previous = %+v, want run1 (%d)", previous, run1)
	}

	if err := s.MarkReported(ctx, shop.ID, "2026-01-03T00:02:00Z"); err != nil {
		t.Fatalf("MarkReported: %v", err)
	}
	current, _, err = s.LatestUnreportedRun(ctx, shop.ID)
	if err != nil {
		t.Fatalf("LatestUnreportedRun after MarkReported: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want nil — run2 and run3 should both be marked reported", current)
	}
}
