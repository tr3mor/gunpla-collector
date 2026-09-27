package store

import (
	"context"
	"testing"

	"gunpla-collector/internal/scraper"
)

// TestSearchSets covers: current price is the most recent price_history row
// (not the first or the cheapest), lowest price is the minimum across all
// history even when it's older than the current row, an inactive
// (deactivated) set is excluded, and the shop-slug filter scopes results.
func TestSearchSets(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	shopA, err := s.GetOrCreateShop(ctx, "shop-a", "Shop A", "https://a.example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop shop-a: %v", err)
	}
	shopB, err := s.GetOrCreateShop(ctx, "shop-b", "Shop B", "https://b.example.com")
	if err != nil {
		t.Fatalf("GetOrCreateShop shop-b: %v", err)
	}

	applyRun := func(shopID int64, startedAt, appliedAt string, sets []scraper.ScrapedSet) int64 {
		t.Helper()
		runID, err := s.StartRun(ctx, shopID, startedAt)
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if err := s.ApplyRun(ctx, shopID, runID, sets, appliedAt); err != nil {
			t.Fatalf("ApplyRun: %v", err)
		}
		return runID
	}

	// Shop A: "Kit One" starts at 5000, drops to 3000 (lowest), then rises
	// back to 4000 (current) — current must be 4000, lowest must be 3000.
	applyRun(shopA.ID, "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z", []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 5000, Currency: "EUR"},
		{ExternalID: "ext-gone", URL: "u-gone", Name: "Kit Gone", Grade: "HG", PriceCents: 1000, Currency: "EUR"},
	})
	applyRun(shopA.ID, "2026-01-02T00:00:00Z", "2026-01-02T00:01:00Z", []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 3000, Currency: "EUR"},
		// ext-gone dropped from this run -> deactivated, must not appear.
	})
	applyRun(shopA.ID, "2026-01-03T00:00:00Z", "2026-01-03T00:01:00Z", []scraper.ScrapedSet{
		{ExternalID: "ext-1", URL: "u1", Name: "Kit One", Grade: "MG", PriceCents: 4000, Currency: "EUR"},
	})

	// Shop B: unrelated set, to check the shop-slug filter.
	applyRun(shopB.ID, "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z", []scraper.ScrapedSet{
		{ExternalID: "ext-b1", URL: "ub1", Name: "Kit B", Grade: "RG", PriceCents: 2000, Currency: "USD"},
	})

	all, err := s.SearchSets(ctx, "")
	if err != nil {
		t.Fatalf("SearchSets(\"\"): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("SearchSets(\"\") = %d rows, want 2 (Kit Gone must be excluded as inactive)", len(all))
	}

	var kitOne *SetSearchRow
	for i := range all {
		if all[i].Name == "Kit One" {
			kitOne = &all[i]
		}
	}
	if kitOne == nil {
		t.Fatalf("Kit One not found in %+v", all)
	}
	if kitOne.CurrentCents != 4000 {
		t.Errorf("Kit One CurrentCents = %d, want 4000 (most recent run)", kitOne.CurrentCents)
	}
	if kitOne.LowestCents != 3000 {
		t.Errorf("Kit One LowestCents = %d, want 3000 (minimum across history)", kitOne.LowestCents)
	}

	scopedA, err := s.SearchSets(ctx, "shop-a")
	if err != nil {
		t.Fatalf("SearchSets(shop-a): %v", err)
	}
	if len(scopedA) != 1 || scopedA[0].Name != "Kit One" {
		t.Errorf("SearchSets(shop-a) = %+v, want exactly [Kit One]", scopedA)
	}

	scopedB, err := s.SearchSets(ctx, "shop-b")
	if err != nil {
		t.Fatalf("SearchSets(shop-b): %v", err)
	}
	if len(scopedB) != 1 || scopedB[0].Name != "Kit B" || scopedB[0].Currency != "USD" {
		t.Errorf("SearchSets(shop-b) = %+v, want exactly [Kit B in USD]", scopedB)
	}
}
