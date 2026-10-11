package store

import (
	"context"
	"testing"

	"gunpla-collector/internal/scraper"
)

// seedTwoShops stores the same kit at two shops (named differently) plus
// one kit only shop A has, and returns the store.
func seedTwoShops(t *testing.T) *Store {
	t.Helper()
	s := openTestStore(t)
	ctx := context.Background()
	for _, sh := range []struct {
		slug string
		sets []scraper.ScrapedSet
	}{
		{"shop-a", []scraper.ScrapedSet{
			{ExternalID: "1", URL: "u1", Name: "MG RX-78-2 Gundam Ver. Ka 1/100", Grade: "MG", PriceCents: 9000, Currency: "EUR"},
			{ExternalID: "2", URL: "u2", Name: "HG Zaku II", Grade: "HG", PriceCents: 2000, Currency: "EUR"},
		}},
		{"shop-b", []scraper.ScrapedSet{
			{ExternalID: "x", URL: "ux", Name: "MG – RX-78-2 Gundam Ver.Ka", Grade: "MG", PriceCents: 8500, Currency: "EUR"},
		}},
	} {
		shop, err := s.GetOrCreateShop(ctx, sh.slug, sh.slug, "https://"+sh.slug)
		if err != nil {
			t.Fatal(err)
		}
		runID, err := s.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyRun(ctx, shop.ID, runID, sh.sets, "2026-01-01T00:01:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestApplyProducts_CreatesReusesAndPrunes(t *testing.T) {
	s := seedTwoShops(t)
	ctx := context.Background()
	listings, err := s.Listings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 3 {
		t.Fatalf("got %d listings, want 3", len(listings))
	}
	for _, l := range listings {
		if l.ProductID != 0 || l.Manual {
			t.Errorf("%q: ProductID=%d Manual=%v before any products", l.Name, l.ProductID, l.Manual)
		}
	}
	id := func(i int) int64 { return listings[i].ID }

	// Two products: the first two sets together, the third alone.
	created, removed, err := s.ApplyProducts(ctx, []ProductPlan{
		{Name: "Kit", Grade: "MG", Members: []ProductMember{{SetID: id(0), Method: "name"}, {SetID: id(1), Method: "name"}}},
		{Name: "Other", Members: []ProductMember{{SetID: id(2)}}},
	}, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 || removed != 0 {
		t.Fatalf("created=%d removed=%d, want 2/0", created, removed)
	}
	listings, _ = s.Listings(ctx)
	first, third := listings[0].ProductID, listings[2].ProductID
	if first == 0 || first != listings[1].ProductID || first == third {
		t.Fatalf("product ids = %d %d %d", first, listings[1].ProductID, third)
	}

	// Keep the first product (renamed) for all three sets: the third
	// set's old product is now empty and is deleted.
	created, removed, err = s.ApplyProducts(ctx, []ProductPlan{
		{ProductID: first, Name: "Everything", Grade: "MG", Members: []ProductMember{{SetID: id(0)}, {SetID: id(1)}, {SetID: id(2)}}},
	}, "2026-01-03T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || removed != 1 {
		t.Fatalf("created=%d removed=%d, want 0/1", created, removed)
	}
	rows, _ := s.SearchSets(ctx, "")
	for _, r := range rows {
		if r.ProductID != first || r.ProductName != "Everything" {
			t.Errorf("%q: product %d %q, want %d Everything", r.Name, r.ProductID, r.ProductName, first)
		}
	}
}

func TestLinkSets_MarksBothManual(t *testing.T) {
	s := seedTwoShops(t)
	ctx := context.Background()
	listings, _ := s.Listings(ctx)
	if _, _, err := s.ApplyProducts(ctx, []ProductPlan{
		{Name: "A", Members: []ProductMember{{SetID: listings[0].ID}}},
		{Name: "B", Members: []ProductMember{{SetID: listings[1].ID}, {SetID: listings[2].ID}}},
	}, "2026-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkSets(ctx, listings[0].ID, listings[1].ID); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Listings(ctx)
	if after[0].ProductID != after[1].ProductID || !after[0].Manual || !after[1].Manual || after[2].Manual {
		t.Errorf("after link: %+v", after)
	}
}

func TestSearchSets_ListingWithoutProductHasZeroID(t *testing.T) {
	s := seedTwoShops(t) // collected but not matched yet
	rows, err := s.SearchSets(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ProductID != 0 {
			t.Errorf("%q: ProductID = %d before matching, want 0", r.Name, r.ProductID)
		}
	}
}
