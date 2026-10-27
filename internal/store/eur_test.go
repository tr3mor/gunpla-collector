package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/scraper"
)

// eurFixture applies runs of one shop, converting with a given rate.
type eurFixture struct {
	t    *testing.T
	s    *Store
	shop Shop
	runs int
}

func newEURFixture(t *testing.T) *eurFixture {
	t.Helper()
	s := openTestStore(t)
	shop, err := s.GetOrCreateShop(context.Background(), "us", "US Shop", "https://us.example.com")
	if err != nil {
		t.Fatal(err)
	}
	return &eurFixture{t: t, s: s, shop: shop}
}

// run applies one run and returns its id.
func (f *eurFixture) run(rates fx.Rates, sets ...scraper.ScrapedSet) int64 {
	f.t.Helper()
	ctx := context.Background()
	f.runs++
	at := "2026-01-0" + string(rune('0'+f.runs)) + "T00:00:00Z"
	id, err := f.s.StartRun(ctx, f.shop.ID, at)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.s.ApplyRun(ctx, f.shop.ID, id, sets, at, rates); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func usd(ext string, cents int) scraper.ScrapedSet {
	return scraper.ScrapedSet{ExternalID: ext, URL: "u" + ext, Name: "Kit " + ext, Grade: "MG", PriceCents: cents, Currency: "USD"}
}

func TestApplyRun_StoresEURValueAndRate(t *testing.T) {
	f := newEURFixture(t)
	f.run(fx.Rates{"USD": 0.9},
		usd("1", 10000),
		scraper.ScrapedSet{ExternalID: "2", URL: "u2", Name: "Kit 2", PriceCents: 5000, Currency: "EUR"},
		scraper.ScrapedSet{ExternalID: "3", URL: "u3", Name: "Kit 3", PriceCents: 700, Currency: "JPY"},
	)
	type row struct {
		eur  sql.NullInt64
		rate sql.NullFloat64
	}
	got := map[string]row{}
	rows, err := f.s.db.Query(`SELECT s.external_id, ph.eur_cents, ph.fx_rate FROM price_history ph JOIN sets s ON s.id = ph.set_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ext string
		var r row
		if err := rows.Scan(&ext, &r.eur, &r.rate); err != nil {
			t.Fatal(err)
		}
		got[ext] = r
	}
	if r := got["1"]; r.eur.Int64 != 9000 || r.rate.Float64 != 0.9 {
		t.Errorf("USD row = %+v, want 9000 at 0.9", r)
	}
	if r := got["2"]; r.eur.Int64 != 5000 || r.rate.Float64 != 1 {
		t.Errorf("EUR row = %+v, want 5000 at 1", r)
	}
	if r := got["3"]; r.eur.Valid || r.rate.Valid {
		t.Errorf("JPY row = %+v, want no EUR value (no rate)", r)
	}
}

func TestSearchSets_ReturnsEURPrices(t *testing.T) {
	f := newEURFixture(t)
	f.run(fx.Rates{"USD": 0.8}, usd("1", 10000)) // €80, the lowest in EUR
	f.run(fx.Rates{"USD": 1.0}, usd("1", 9500))  // lower in USD, but €95
	rows, err := f.s.SearchSets(context.Background(), "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	r := rows[0]
	if !r.HasEUR || r.EURCents != 9500 || r.LowestEURCents != 8000 || r.CurrentCents != 9500 || r.LowestCents != 9500 || r.Currency != "USD" {
		t.Errorf("row = %+v", r)
	}
}

func TestSearchSets_NoRateMeansNoEURValue(t *testing.T) {
	f := newEURFixture(t)
	f.run(nil, usd("1", 10000))
	rows, err := f.s.SearchSets(context.Background(), "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	if r := rows[0]; r.HasEUR || r.EURCents != 0 || r.CurrentCents != 10000 {
		t.Errorf("row = %+v, want the USD price and no EUR value", r)
	}
}

// An unchanged shop price is not a price change just because the rate moved.
func TestPriceChanges_IgnoresRateDrift_ReportsEUR(t *testing.T) {
	f := newEURFixture(t)
	ctx := context.Background()
	r1 := f.run(fx.Rates{"USD": 0.90}, usd("same", 10000), usd("cut", 10000))
	r2 := f.run(fx.Rates{"USD": 0.92}, usd("same", 10000), usd("cut", 8000))

	changes, err := f.s.PriceChanges(ctx, f.shop.ID, r2, r1)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %+v, want only the real price cut", changes)
	}
	if c := changes[0]; c.Name != "Kit cut" || c.Currency != "EUR" || c.OldCents != 9000 || c.NewCents != 7360 {
		t.Errorf("change = %+v, want €90.00 -> €73.60", c)
	}
}

func TestPriceChanges_FallsBackToShopCurrencyWithoutEUR(t *testing.T) {
	f := newEURFixture(t)
	r1 := f.run(nil, usd("1", 10000))
	r2 := f.run(nil, usd("1", 8000))
	changes, err := f.s.PriceChanges(context.Background(), f.shop.ID, r2, r1)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes = %+v, err = %v", changes, err)
	}
	if c := changes[0]; c.Currency != "USD" || c.OldCents != 10000 || c.NewCents != 8000 {
		t.Errorf("change = %+v, want the USD prices", c)
	}
}

func TestNewAndRemovedSets_ReportEURWhenKnown(t *testing.T) {
	f := newEURFixture(t)
	ctx := context.Background()
	r1 := f.run(fx.Rates{"USD": 0.9}, usd("old", 2000))
	r2 := f.run(fx.Rates{"USD": 0.9}, usd("new", 10000))
	added, err := f.s.NewSets(ctx, f.shop.ID, r2, r1)
	if err != nil || len(added) != 1 || added[0].Currency != "EUR" || added[0].PriceCents != 9000 {
		t.Errorf("new = %+v, err = %v", added, err)
	}
	gone, err := f.s.RemovedSets(ctx, f.shop.ID, r2, r1)
	if err != nil || len(gone) != 1 || gone[0].Currency != "EUR" || gone[0].PriceCents != 1800 {
		t.Errorf("removed = %+v, err = %v", gone, err)
	}
}

func TestBackfillEURAndLastRate(t *testing.T) {
	f := newEURFixture(t)
	ctx := context.Background()
	f.run(nil, usd("1", 10000)) // stored without a rate

	if _, ok, err := f.s.LastRate(ctx, "USD"); err != nil || ok {
		t.Fatalf("LastRate before any rate: ok = %v, err = %v", ok, err)
	}
	n, err := f.s.BackfillEUR(ctx, fx.Rates{"USD": 0.9})
	if err != nil || n != 1 {
		t.Fatalf("BackfillEUR = %d, %v; want 1 row", n, err)
	}
	rows, _ := f.s.SearchSets(ctx, "")
	if len(rows) != 1 || !rows[0].HasEUR || rows[0].EURCents != 9000 {
		t.Errorf("after backfill: %+v", rows)
	}
	if rate, ok, err := f.s.LastRate(ctx, "USD"); err != nil || !ok || rate != 0.9 {
		t.Errorf("LastRate = %v, %v, %v; want 0.9", rate, ok, err)
	}
	// Rows that already have a value are left alone.
	if n, err := f.s.BackfillEUR(ctx, fx.Rates{"USD": 0.5}); err != nil || n != 0 {
		t.Errorf("second BackfillEUR = %d, %v; want 0", n, err)
	}
}

// Migration 9 fills in EUR rows exactly and leaves other currencies for
// BackfillEUR, without touching the stored prices.
func TestMigrate_EURPricesOnExistingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := migrationsFS.ReadFile("migrations/0001_baseline.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		string(baseline),
		`INSERT INTO shops (id, slug, name, base_url) VALUES (1, 's', 'S', 'u')`,
		`INSERT INTO scrape_runs (id, shop_id, started_at, status) VALUES (1, 1, 't', 'success')`,
		`INSERT INTO sets (id, shop_id, external_id, url, name, first_seen_at, last_seen_at) VALUES (1, 1, 'e1', 'u', 'n', 't', 't'), (2, 1, 'e2', 'u', 'n', 't', 't')`,
		`INSERT INTO price_history (set_id, run_id, price_cents, currency, scraped_at) VALUES (1, 1, 5000, 'EUR', 't'), (2, 1, 10000, 'USD', 't')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check := func(set int, wantEUR sql.NullInt64) {
		t.Helper()
		var got sql.NullInt64
		if err := s.db.QueryRow(`SELECT eur_cents FROM price_history WHERE set_id = ?`, set).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != wantEUR {
			t.Errorf("set %d eur_cents = %v, want %v", set, got, wantEUR)
		}
	}
	check(1, sql.NullInt64{Int64: 5000, Valid: true})
	check(2, sql.NullInt64{})
	if _, err := s.BackfillEUR(context.Background(), fx.Rates{"USD": 0.9}); err != nil {
		t.Fatal(err)
	}
	check(2, sql.NullInt64{Int64: 9000, Valid: true})
}

// A Gundam Store price (Dutch market, DKK) converts at the stored DKK rate,
// both when a run stores it and when the backfill fills a row stored without
// a rate.
func TestApplyRun_ConvertsDKKPrice(t *testing.T) {
	dkk := func(ext string) scraper.ScrapedSet {
		return scraper.ScrapedSet{ExternalID: ext, URL: "u" + ext, Name: "Kit " + ext, Grade: "MG", PriceCents: 69694, Currency: "DKK"}
	}
	f := newEURFixture(t)
	ctx := context.Background()
	f.run(fx.Rates{"DKK": 0.134}, dkk("1"))

	rows, err := f.s.SearchSets(ctx, "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("SearchSets = %+v, %v", rows, err)
	}
	if r := rows[0]; !r.HasEUR || r.EURCents != 9339 || r.CurrentCents != 69694 || r.Currency != "DKK" {
		t.Errorf("row = %+v, want 69694 DKK as 9339 EUR", r)
	}
	if rate, ok, err := f.s.LastRate(ctx, "DKK"); err != nil || !ok || rate != 0.134 {
		t.Errorf("LastRate = %v, %v, %v; want 0.134", rate, ok, err)
	}

	f.run(nil, dkk("2")) // stored while the rate was missing
	if n, err := f.s.BackfillEUR(ctx, fx.Rates{"DKK": 0.134}); err != nil || n != 1 {
		t.Fatalf("BackfillEUR = %d, %v; want 1 row", n, err)
	}
	rows, _ = f.s.SearchSets(ctx, "")
	for _, r := range rows {
		if !r.HasEUR || r.EURCents != 9339 {
			t.Errorf("after backfill: %+v, want 9339 EUR", r)
		}
	}
}
