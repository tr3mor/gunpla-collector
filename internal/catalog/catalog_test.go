package catalog

import (
	"context"
	"path/filepath"
	"testing"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// seedTwoShops stores the same kit at two shops (named differently) plus
// one kit only shop A has.
func seedTwoShops(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
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

func TestRegroup_GroupsAcrossShopsAndKeepsIDsStable(t *testing.T) {
	s := seedTwoShops(t)
	c := New(s)
	ctx := context.Background()

	rep, err := c.Regroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Products != 2 || rep.MultiShop != 1 || rep.Created != 2 || rep.Listings != 3 {
		t.Fatalf("report = %+v, want 2 products, 1 multi-shop, 2 created, 3 listings", rep)
	}

	rows, err := s.SearchSets(ctx, "")
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
	// The product is named after the shortest listing name.
	for _, r := range rows {
		if r.Name == "HG Zaku II" && r.ProductName != "HG Zaku II" {
			t.Errorf("product name = %q", r.ProductName)
		}
		if r.ShopSlug == "shop-a" && r.Grade == "MG" && r.ProductName != "MG – RX-78-2 Gundam Ver.Ka" {
			t.Errorf("multi-shop product name = %q, want the shorter listing name", r.ProductName)
		}
	}

	// A second run changes nothing: no new products, nobody reassigned.
	again, err := c.Regroup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Created != 0 || again.Removed != 0 || again.Reassigned != 0 {
		t.Errorf("re-run not idempotent: %+v", again)
	}
}

func TestLinkAndUnlinkSurviveRegroup(t *testing.T) {
	s := seedTwoShops(t)
	c := New(s)
	ctx := context.Background()
	if _, err := c.Regroup(ctx); err != nil {
		t.Fatal(err)
	}

	listings, _ := s.Listings(ctx)
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
	if err := c.Unlink(ctx, kaB); err != nil {
		t.Fatal(err)
	}
	// And force an unrelated kit in with the first one.
	if err := c.Link(ctx, ka, zaku); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Regroup(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.SearchSets(ctx, "")
	byName := map[string]store.SetSearchRow{}
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
