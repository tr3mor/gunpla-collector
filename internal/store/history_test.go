package store

import (
	"context"
	"errors"
	"testing"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/scraper"
)

// collectOnce runs one successful collect for the shop.
func collectOnce(t *testing.T, s *Store, shopID int64, at string, rates fx.Rates, sets ...scraper.ScrapedSet) int64 {
	t.Helper()
	ctx := context.Background()
	runID, err := s.StartRun(ctx, shopID, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyRun(ctx, shopID, runID, sets, at, rates); err != nil {
		t.Fatal(err)
	}
	return runID
}

func TestHistory_OneRecordPerRunAndInactiveListings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	shop, err := s.GetOrCreateShop(ctx, "a", "Shop A", "https://a")
	if err != nil {
		t.Fatal(err)
	}
	kit := func(price int, av scraper.Availability) scraper.ScrapedSet {
		return scraper.ScrapedSet{ExternalID: "1", URL: "u1", Name: "MG Kit", Grade: "MG", PriceCents: price, Currency: "EUR", Availability: av}
	}
	r1 := collectOnce(t, s, shop.ID, "2026-01-01T18:00:00Z", nil, kit(5000, scraper.AvailabilityInStock))
	r2 := collectOnce(t, s, shop.ID, "2026-01-02T18:00:00Z", nil, kit(5000, scraper.AvailabilityOutOfStock))
	// A third run without the kit retires it.
	collectOnce(t, s, shop.ID, "2026-01-03T18:00:00Z", nil, scraper.ScrapedSet{ExternalID: "2", URL: "u2", Name: "HG Other", Grade: "HG", PriceCents: 1000, Currency: "EUR"})

	listings, err := s.Listings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var kitID int64
	for _, l := range listings {
		if l.Name == "MG Kit" {
			kitID = l.ID
			if l.Active {
				t.Fatal("kit should be inactive after the third run")
			}
		}
	}

	got, err := s.History(ctx, 0, kitID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d listings, want 1", len(got))
	}
	h := got[0]
	if h.Active || h.ShopName != "Shop A" || h.Name != "MG Kit" || h.URL != "u1" {
		t.Errorf("listing = %+v", h)
	}
	if len(h.Records) != 2 || h.Records[0].RunID != r1 || h.Records[1].RunID != r2 {
		t.Fatalf("records = %+v, want one per run %d and %d", h.Records, r1, r2)
	}
	if h.Records[1].Availability != scraper.AvailabilityOutOfStock || h.Records[0].EURCents != 5000 || !h.Records[0].HasEUR {
		t.Errorf("records = %+v", h.Records)
	}
	if len(h.Runs) != 3 || h.Runs[0].ID != r1 {
		t.Errorf("runs = %+v, want the shop's 3 successful runs", h.Runs)
	}
}

func TestHistory_RecordWithoutEURValueIsKeptWithoutIt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	shop, err := s.GetOrCreateShop(ctx, "a", "Shop A", "https://a")
	if err != nil {
		t.Fatal(err)
	}
	// No rate for CHF, so the row has no EUR value.
	collectOnce(t, s, shop.ID, "2026-01-01T18:00:00Z", fx.Rates{"USD": 0.9},
		scraper.ScrapedSet{ExternalID: "1", URL: "u", Name: "MG Kit", PriceCents: 4200, Currency: "CHF"})

	got, err := s.History(ctx, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r := got[0].Records[0]; r.HasEUR || r.EURCents != 0 || r.PriceCents != 4200 || r.Currency != "CHF" {
		t.Errorf("record = %+v, want shop price kept and no EUR value", r)
	}
}

func TestHistory_ProductListingsOrderedByShopThenName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	zeon, _ := s.GetOrCreateShop(ctx, "zeon", "Zeon", "https://z")
	alpha, _ := s.GetOrCreateShop(ctx, "alpha", "Alpha", "https://a")
	collectOnce(t, s, zeon.ID, "2026-01-01T18:00:00Z", nil, scraper.ScrapedSet{ExternalID: "1", URL: "z1", Name: "Kit", PriceCents: 100, Currency: "EUR"})
	collectOnce(t, s, alpha.ID, "2026-01-01T18:05:00Z", nil,
		scraper.ScrapedSet{ExternalID: "2", URL: "a2", Name: "Kit B", PriceCents: 100, Currency: "EUR"},
		scraper.ScrapedSet{ExternalID: "1", URL: "a1", Name: "Kit A", PriceCents: 100, Currency: "EUR"},
		scraper.ScrapedSet{ExternalID: "3", URL: "a3", Name: "Unrelated", PriceCents: 100, Currency: "EUR"})

	listings, _ := s.Listings(ctx)
	var pid int64
	err := s.InProductTx(ctx, func(tx *ProductTx) error {
		var err error
		if pid, err = tx.CreateProduct(ctx, "Kit", "", "2026-01-01T00:00:00Z"); err != nil {
			return err
		}
		for _, l := range listings {
			if l.Name != "Unrelated" {
				if err := tx.AssignSet(ctx, l.ID, pid, "name"); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.History(ctx, pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, h := range got {
		names = append(names, h.ShopName+"/"+h.Name)
	}
	want := []string{"Alpha/Kit A", "Alpha/Kit B", "Zeon/Kit"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
}

func TestHistory_UnknownKitIsNotFound(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.History(ctx, 99, 0); !errors.Is(err, ErrKitNotFound) {
		t.Errorf("unknown product: err = %v, want ErrKitNotFound", err)
	}
	if _, err := s.History(ctx, 0, 99); !errors.Is(err, ErrKitNotFound) {
		t.Errorf("unknown listing: err = %v, want ErrKitNotFound", err)
	}
}
