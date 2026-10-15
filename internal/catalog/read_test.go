package catalog

import (
	"context"
	"errors"
	"slices"
	"testing"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// rowsStore serves fixed search rows; the other Store methods are unused
// by the read model. Like the real store, it gives EUR rows a EUR value.
type rowsStore struct {
	Store
	rows []store.SetSearchRow
}

func (f *rowsStore) SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error) {
	var out []store.SetSearchRow
	for _, r := range f.rows {
		if r.Currency == "EUR" && !r.HasEUR {
			r.EURCents, r.LowestEURCents, r.HasEUR = r.CurrentCents, r.LowestCents, true
		}
		if shopSlug == "" || r.ShopSlug == shopSlug {
			out = append(out, r)
		}
	}
	return out, nil
}

func searchRows() []store.SetSearchRow {
	return []store.SetSearchRow{
		{ShopSlug: "shop-a", Name: "RX-78-2 Gundam", Grade: "MG", CurrentCents: 4000, Currency: "EUR"},
		{ShopSlug: "shop-a", Name: "Zaku II", Grade: "HG", CurrentCents: 2000, Currency: "EUR"},
		{ShopSlug: "shop-b", Name: "RX-93 Nu Gundam", Grade: "RG", CurrentCents: 3000, Currency: "USD"},
		{ShopSlug: "shop-b", Name: "Sold Out Kit", Grade: "HG", CurrentCents: 1000, Currency: "USD", Availability: scraper.AvailabilityOutOfStock},
		{ShopSlug: "shop-a", Name: "Ungraded Pre", CurrentCents: 1500, Currency: "EUR", Availability: scraper.AvailabilityPreorder},
	}
}

func names(rows []store.SetSearchRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestSearch_Filters(t *testing.T) {
	c := New(&rowsStore{rows: searchRows()})
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		f    Filter
		want []string
	}{
		{"default hides out of stock", Filter{}, []string{"RX-78-2 Gundam", "Zaku II", "RX-93 Nu Gundam", "Ungraded Pre"}},
		{"show out of stock", Filter{ShowOutOfStock: true}, []string{"RX-78-2 Gundam", "Zaku II", "RX-93 Nu Gundam", "Sold Out Kit", "Ungraded Pre"}},
		{"substring is case-insensitive", Filter{Query: "NU"}, []string{"RX-93 Nu Gundam"}},
		{"regexp is case-insensitive", Filter{Regexp: "zaku"}, []string{"Zaku II"}},
		{"query and regexp are ANDed", Filter{Query: "gundam", Regexp: "^RX-78"}, []string{"RX-78-2 Gundam"}},
		{"shop", Filter{Shop: "shop-b"}, []string{"RX-93 Nu Gundam"}},
		{"grade is case-insensitive and hides sold out", Filter{Grade: "hg"}, []string{"Zaku II"}},
		{"grade none", Filter{Grade: "none"}, []string{"Ungraded Pre"}},
		{"grade with no match", Filter{Grade: "PG"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := c.Search(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			got := names(rows)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestSearch_InvalidRegexpIsAnInvalidFilterError(t *testing.T) {
	c := New(&rowsStore{rows: searchRows()})
	_, err := c.Search(context.Background(), Filter{Regexp: "(unclosed"})
	var bad *InvalidFilterError
	if !errors.As(err, &bad) {
		t.Fatalf("err = %v, want InvalidFilterError", err)
	}
	if _, err := c.Products(context.Background(), Filter{Regexp: "("}); !errors.As(err, &bad) {
		t.Fatalf("Products err = %v, want InvalidFilterError", err)
	}
}

func TestProducts_GroupsListingsAndMarksCheapest(t *testing.T) {
	c := New(&rowsStore{rows: []store.SetSearchRow{
		{SetID: 1, ProductID: 7, ProductName: "RX-78-2 Gundam", ShopSlug: "shop-a", Name: "MG RX-78-2 Gundam Ver.Ka 1/100", Grade: "MG", CurrentCents: 9000, Currency: "EUR"},
		{SetID: 2, ProductID: 7, ProductName: "RX-78-2 Gundam", ShopSlug: "shop-b", Name: "MG – RX-78-2 Gundam Ver.Ka", Grade: "MG", CurrentCents: 8500, Currency: "EUR"},
		{SetID: 3, ProductID: 7, ProductName: "RX-78-2 Gundam", ShopSlug: "shop-c", Name: "RX-78-2 Gundam Ver.Ka", Grade: "MG", CurrentCents: 7000, Currency: "EUR", Availability: scraper.AvailabilityOutOfStock},
		{SetID: 4, ProductID: 8, ProductName: "Zaku II", ShopSlug: "shop-a", Name: "HG Zaku II", Grade: "HG", CurrentCents: 2000, Currency: "EUR"},
		{SetID: 5, ShopSlug: "shop-a", Name: "Unmatched Kit", Grade: "HG", CurrentCents: 1000, Currency: "EUR"},
	}})
	ctx := context.Background()

	got, err := c.Products(ctx, Filter{ShowOutOfStock: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d products, want 3 (grouped, plus 2 singles)", len(got))
	}
	if got[0].ID != 7 || len(got[0].Listings) != 3 {
		t.Fatalf("multi-shop product should sort first with 3 listings: %+v", got[0])
	}
	// Listings run cheapest first; the out-of-stock €70 one goes last even
	// though it is the lowest price.
	var order []string
	for _, l := range got[0].Listings {
		order = append(order, l.ShopSlug)
	}
	if want := []string{"shop-b", "shop-a", "shop-c"}; !slices.Equal(order, want) {
		t.Errorf("listing order = %v, want %v", order, want)
	}
	for _, l := range got[0].Listings {
		// The out-of-stock €70 listing is cheaper but can't be bought.
		if want := l.ShopSlug == "shop-b"; l.Cheapest != want {
			t.Errorf("%s: cheapest = %v, want %v", l.ShopSlug, l.Cheapest, want)
		}
	}
	// A listing without a product stands alone, named after itself.
	if single := got[1]; single.ID != 0 || single.Name != "Unmatched Kit" || single.Listings[0].Cheapest {
		t.Errorf("unmatched listing = %+v", single)
	}

	// Default hides out-of-stock listings, shrinking the group.
	if got, _ := c.Products(ctx, Filter{}); len(got[0].Listings) != 2 {
		t.Errorf("default view has %d listings in group, want 2", len(got[0].Listings))
	}
	// A name filter keeps the whole product if one listing matches.
	if got, _ := c.Products(ctx, Filter{Query: "zaku"}); len(got) != 1 || got[0].ID != 8 {
		t.Errorf("q=zaku: %+v", got)
	}
}

func TestProducts_ComparesInEUR(t *testing.T) {
	c := New(&rowsStore{rows: []store.SetSearchRow{
		// $100 stored as €90: cheaper than the €95 listing despite the bigger number.
		{SetID: 1, ProductID: 7, ShopSlug: "us", Name: "Kit", CurrentCents: 10000, Currency: "USD", EURCents: 9000, HasEUR: true},
		{SetID: 2, ProductID: 7, ShopSlug: "nl", Name: "Kit", CurrentCents: 9500, Currency: "EUR", EURCents: 9500, HasEUR: true},
		// One shop only: nothing to compare, so no cheapest flag.
		{SetID: 3, ProductID: 8, ShopSlug: "us", Name: "Solo", CurrentCents: 1000, Currency: "USD", EURCents: 900, HasEUR: true},
		// A listing with no EUR value is not comparable.
		{SetID: 4, ProductID: 9, ShopSlug: "us", Name: "Odd", CurrentCents: 1, Currency: "JPY"},
		{SetID: 5, ProductID: 9, ShopSlug: "nl", Name: "Odd", CurrentCents: 9999, Currency: "EUR", EURCents: 9999, HasEUR: true},
	}})

	got, err := c.Products(context.Background(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	flags := map[string]bool{}
	for _, p := range got {
		for _, l := range p.Listings {
			flags[p.Name+"/"+l.ShopSlug] = l.Cheapest
		}
	}
	want := map[string]bool{"Kit/us": true, "Kit/nl": false, "Solo/us": false, "Odd/us": false, "Odd/nl": true}
	for k, w := range want {
		if flags[k] != w {
			t.Errorf("%s: cheapest = %v, want %v", k, flags[k], w)
		}
	}
}
