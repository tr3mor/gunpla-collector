package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

func TestLoadRates_LiveThenLastStoredThenFallback(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	down := func(context.Context) (fx.Rates, error) { return nil, errors.New("ecb down") }
	up := func(context.Context) (fx.Rates, error) { return fx.Rates{"USD": 0.91}, nil }

	// Nothing stored yet and the lookup fails: the configured fallback.
	if got := loadRates(ctx, db, down, 0.89, logger); got["USD"] != 0.89 {
		t.Errorf("first run, ECB down: got %v, want the 0.89 fallback", got)
	}
	// The live rate wins, and a run stores prices with it.
	if got := loadRates(ctx, db, up, 0.89, logger); got["USD"] != 0.91 {
		t.Errorf("ECB up: got %v, want 0.91", got)
	}
	shop, _ := db.GetOrCreateShop(ctx, "us", "US", "https://us")
	run, _ := db.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	set := scraper.ScrapedSet{ExternalID: "1", URL: "u", Name: "Kit", PriceCents: 10000, Currency: "USD"}
	if err := db.ApplyRun(ctx, shop.ID, run, []scraper.ScrapedSet{set}, "2026-01-01T00:01:00Z", fx.Rates{"USD": 0.91}); err != nil {
		t.Fatal(err)
	}
	// ECB down again: reuse what the last run stored, not the fallback.
	if got := loadRates(ctx, db, down, 0.89, logger); got["USD"] != 0.91 {
		t.Errorf("ECB down after a run: got %v, want the stored 0.91", got)
	}
}

func TestLoadRates_BackfillsOlderRows(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	shop, _ := db.GetOrCreateShop(ctx, "us", "US", "https://us")
	run, _ := db.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	set := scraper.ScrapedSet{ExternalID: "1", URL: "u", Name: "Kit", PriceCents: 10000, Currency: "USD"}
	if err := db.ApplyRun(ctx, shop.ID, run, []scraper.ScrapedSet{set}, "2026-01-01T00:01:00Z", nil); err != nil {
		t.Fatal(err)
	}
	loadRates(ctx, db, func(context.Context) (fx.Rates, error) { return fx.Rates{"USD": 0.9}, nil }, 0.89, logger)
	rows, err := db.SearchSets(ctx, "")
	if err != nil || len(rows) != 1 || !rows[0].HasEUR || rows[0].EURCents != 9000 {
		t.Errorf("rows = %+v, err = %v; want the old USD row backfilled to €90.00", rows, err)
	}
}
