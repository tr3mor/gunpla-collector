package catalog

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// Filter narrows the active listings a query returns. The zero value
// matches everything that is in stock or of unknown stock.
type Filter struct {
	// Query is a case-insensitive substring of the listing name.
	Query string
	// Regexp is a case-insensitive regular expression on the listing name.
	Regexp string
	// Shop is a shop slug.
	Shop string
	// Grade is MG/HG/RG/PG (case-insensitive), or "none" for listings with
	// no grade (e.g. most pre-orders).
	Grade string
	// ShowOutOfStock includes listings the shop reports as out of stock.
	ShowOutOfStock bool
}

// InvalidFilterError reports a Filter that can't be applied, e.g. a
// regular expression that doesn't compile. Callers serving user input
// treat it as a bad request.
type InvalidFilterError struct{ Err error }

func (e *InvalidFilterError) Error() string { return e.Err.Error() }
func (e *InvalidFilterError) Unwrap() error { return e.Err }

// Search returns the active listings that pass f, ordered by name.
func (c *Catalog) Search(ctx context.Context, f Filter) ([]store.SetSearchRow, error) {
	var re *regexp.Regexp
	if f.Regexp != "" {
		var err error
		// (?i) is harmless to prepend even if the caller wrote their own flags.
		if re, err = regexp.Compile("(?i)" + f.Regexp); err != nil {
			return nil, &InvalidFilterError{Err: err}
		}
	}
	query := strings.ToLower(f.Query)

	all, err := c.store.SearchSets(ctx, f.Shop)
	if err != nil {
		return nil, err
	}
	var rows []store.SetSearchRow
	for _, row := range all {
		if query != "" && !strings.Contains(strings.ToLower(row.Name), query) {
			continue
		}
		if f.Grade != "" {
			if strings.EqualFold(f.Grade, "none") {
				if row.Grade != "" {
					continue
				}
			} else if !strings.EqualFold(row.Grade, f.Grade) {
				continue
			}
		}
		if re != nil && !re.MatchString(row.Name) {
			continue
		}
		if !f.ShowOutOfStock && row.Availability == scraper.AvailabilityOutOfStock {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Product is one kit with every shop's listing of it.
type Product struct {
	// ID is 0 for a listing the matcher hasn't grouped yet; it stands alone.
	ID       int64
	Name     string
	Grade    string
	Listings []Listing
}

// Listing is one shop's entry for a product.
type Listing struct {
	store.SetSearchRow
	// Cheapest marks the lowest-priced buyable listing of a kit sold by two
	// or more shops (all of them, on a tie).
	Cheapest bool
}

// Products is Search with listings of the same kit grouped together. A kit
// is returned if any of its listings passes f, and with every listing that
// does, so the cross-shop price comparison stays visible. Kits sold by more
// shops come first.
func (c *Catalog) Products(ctx context.Context, f Filter) ([]Product, error) {
	rows, err := c.Search(ctx, f)
	if err != nil {
		return nil, err
	}

	byKey := map[int64]*Product{}
	var order []int64
	for _, row := range rows {
		// A listing without a product stands alone, keyed by a negative id
		// that can't collide with a product id.
		key := row.ProductID
		if key == 0 {
			key = -row.SetID
		}
		p, ok := byKey[key]
		if !ok {
			name := row.ProductName
			if name == "" {
				name = row.Name
			}
			p = &Product{ID: row.ProductID, Name: name, Grade: row.Grade}
			byKey[key] = p
			order = append(order, key)
		}
		p.Listings = append(p.Listings, Listing{SetSearchRow: row})
	}

	out := make([]Product, 0, len(order))
	for _, key := range order {
		p := byKey[key]
		c.markCheapest(p.Listings)
		out = append(out, *p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Listings) != len(out[j].Listings) {
			return len(out[i].Listings) > len(out[j].Listings)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// markCheapest flags the cheapest in-stock (or pre-order) listing, but only
// when at least two shops list the kit — a lone listing is trivially
// "cheapest". Stock, currencies and missing rates are fx.Rates.Cheapest's
// concern.
func (c *Catalog) markCheapest(ls []Listing) {
	shops := map[string]bool{}
	for _, l := range ls {
		shops[l.ShopSlug] = true
	}
	if len(shops) < 2 {
		return
	}
	offers := make([]fx.Offer, len(ls))
	for i, l := range ls {
		offers[i] = fx.Offer{Cents: l.CurrentCents, Currency: l.Currency, Excluded: l.Availability == scraper.AvailabilityOutOfStock}
	}
	for i, cheapest := range c.rates.Cheapest(offers) {
		ls[i].Cheapest = cheapest
	}
}

// Display presents a listing price in EUR when a rate exists, keeping the
// original (see fx.Rates.Display). Consumers show prices through the
// catalog so the rates are wired once.
func (c *Catalog) Display(cents int, currency string) fx.Price {
	return c.rates.Display(cents, currency)
}
