package store

import (
	"context"
	"errors"
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
		if err := s.ApplyRun(ctx, shop.ID, runID, sh.sets, "2026-01-01T00:01:00Z", nil); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestInProductTx_CommitsOnNilAndRollsBackOnError(t *testing.T) {
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
		if !l.Active || l.ProductID != 0 || l.Method != "" {
			t.Errorf("%q before products: %+v", l.Name, l)
		}
	}

	assign := func(fail bool) error {
		return s.InProductTx(ctx, func(tx *ProductTx) error {
			pid, err := tx.CreateProduct(ctx, "Kit", "MG", "2026-01-02T00:00:00Z")
			if err != nil {
				return err
			}
			if err := tx.AssignSet(ctx, listings[0].ID, pid, "name"); err != nil {
				return err
			}
			if fail {
				return errors.New("boom")
			}
			return nil
		})
	}
	if err := assign(true); err == nil {
		t.Fatal("want the callback error")
	}
	if after, _ := s.Listings(ctx); after[0].ProductID != 0 {
		t.Errorf("failed tx left product %d behind", after[0].ProductID)
	}

	if err := assign(false); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Listings(ctx)
	if after[0].ProductID == 0 || after[0].Method != "name" {
		t.Errorf("committed tx: %+v", after[0])
	}

	// Pruning drops products nothing points at.
	var pruned int
	err = s.InProductTx(ctx, func(tx *ProductTx) error {
		if _, err := tx.CreateProduct(ctx, "Empty", "", "2026-01-03T00:00:00Z"); err != nil {
			return err
		}
		pruned, err = tx.PruneProducts(ctx)
		return err
	})
	if err != nil || pruned != 1 {
		t.Errorf("pruned=%d err=%v, want 1 (only the empty product)", pruned, err)
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
