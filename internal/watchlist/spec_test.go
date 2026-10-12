package watchlist

import (
	"context"
	"testing"

	"gunpla-collector/internal/store"
)

// rowsStore serves fixed search rows; like the real store, it gives EUR rows
// a EUR value.
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
		out = append(out, r)
	}
	return out, nil
}

func watchRows() []store.SetSearchRow {
	row := func(id int64, shop, name string, cents int) store.SetSearchRow {
		return store.SetSearchRow{SetID: id, ShopSlug: shop, ShopName: shop, Name: name, Grade: "MG", CurrentCents: cents, Currency: "EUR"}
	}
	return []store.SetSearchRow{
		row(1, "a", "MG 1/100 Full Armor ZZ Gundam Ver.Ka", 9000),     // no model number
		row(2, "b", "MG – FA-010S Full Armor ZZ Gundam Ver.Ka", 8500), // same kit, model number printed
		row(3, "a", "MG FA-010S Full Armor ZZ Gundam 1/100", 5000),    // same suit, base release
		row(4, "b", "MG – FA-010A FAZZ Ver.Ka", 7000),                 // a different suit
	}
}

// The same kit must be found whichever of its listings you start from, even
// when only some shops print the model number.
func TestPreviewWatch_SameKitFromEitherListing(t *testing.T) {
	c := New(&rowsStore{rows: watchRows()})
	ctx := context.Background()
	for _, start := range []int64{1, 2} {
		spec, _, err := c.SpecFromSet(ctx, start)
		if err != nil {
			t.Fatal(err)
		}
		p, err := c.Preview(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, m := range p.Matches {
			ids = append(ids, m.SetID)
		}
		if len(ids) != 2 || (ids[0] != 2 || ids[1] != 1) { // cheapest first
			t.Errorf("start %d: matches = %v, want [2 1]", start, ids)
		}
		if len(p.Variants) != 2 {
			t.Errorf("start %d: variants = %+v, want Ver.Ka and the base release", start, p.Variants)
		}
		if !p.Matches[0].Best {
			t.Errorf("start %d: cheapest listing not marked best", start)
		}
	}
}
