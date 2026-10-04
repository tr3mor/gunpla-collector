package store

import (
	"context"
	"testing"

	"gunpla-collector/internal/matcher"
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

func runMatching(t *testing.T, s *Store) MatchStats {
	t.Helper()
	ctx := context.Background()
	listings, err := s.MatchListings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	groups, _ := matcher.Match(listings)
	stats, err := s.ApplyMatches(ctx, listings, groups, "2026-01-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestApplyMatches_GroupsAcrossShopsAndKeepsIDsStable(t *testing.T) {
	s := seedTwoShops(t)
	stats := runMatching(t, s)
	if stats.Products != 2 || stats.MultiShop != 1 || stats.Created != 2 {
		t.Fatalf("stats = %+v, want 2 products, 1 multi-shop, 2 created", stats)
	}

	rows, err := s.SearchSets(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	pid := map[string]int64{}
	for _, r := range rows {
		if r.ProductID == 0 {
			t.Errorf("%q has no product", r.Name)
		}
		pid[r.ShopSlug+"/"+r.Name] = r.ProductID
	}
	if pid["shop-a/MG RX-78-2 Gundam Ver. Ka 1/100"] != pid["shop-b/MG – RX-78-2 Gundam Ver.Ka"] {
		t.Errorf("same kit got different products: %v", pid)
	}

	// A second run changes nothing: no new products, nobody reassigned.
	again := runMatching(t, s)
	if again.Created != 0 || again.Removed != 0 || again.Reassigned != 0 {
		t.Errorf("re-run not idempotent: %+v", again)
	}
}

func TestLinkAndUnlinkSurviveRematching(t *testing.T) {
	s := seedTwoShops(t)
	ctx := context.Background()
	runMatching(t, s)

	listings, _ := s.MatchListings(ctx)
	idOf := func(name string) int64 {
		for _, l := range listings {
			if l.Name == name {
				return l.ID
			}
		}
		t.Fatalf("no listing %q", name)
		return 0
	}
	ka, kaB, zaku := idOf("MG RX-78-2 Gundam Ver. Ka 1/100"), idOf("MG – RX-78-2 Gundam Ver.Ka"), idOf("HG Zaku II")

	// Wrongly merged by the matcher in this scenario: split them by hand.
	if err := s.UnlinkSet(ctx, kaB, "2026-01-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// And force an unrelated kit in with the first one.
	if err := s.LinkSets(ctx, ka, zaku); err != nil {
		t.Fatal(err)
	}

	runMatching(t, s)
	rows, _ := s.SearchSets(ctx, "")
	byName := map[string]SetSearchRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if byName["MG – RX-78-2 Gundam Ver.Ka"].ProductID == byName["MG RX-78-2 Gundam Ver. Ka 1/100"].ProductID {
		t.Error("unlinked set was re-merged by automatic matching")
	}
	if byName["HG Zaku II"].ProductID != byName["MG RX-78-2 Gundam Ver. Ka 1/100"].ProductID {
		t.Error("manual link was undone by automatic matching")
	}
	if byName["HG Zaku II"].MatchMethod != "manual" {
		t.Errorf("match method = %q, want manual", byName["HG Zaku II"].MatchMethod)
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
