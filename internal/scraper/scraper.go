// Package scraper defines the Scraper interface implemented by each
// supported shop, plus a registry so collector/reporter never need to know
// about individual shop implementations.
package scraper

import (
	"context"
	"sort"
)

// Availability is whether a set can be bought now, is out of stock, or is
// open for pre-order. The empty value means the shop doesn't say.
type Availability string

const (
	AvailabilityUnknown    Availability = ""
	AvailabilityInStock    Availability = "in_stock"
	AvailabilityOutOfStock Availability = "out_of_stock"
	AvailabilityPreorder   Availability = "preorder"
)

// AvailabilityFromBool maps a shop's plain in-stock flag.
func AvailabilityFromBool(inStock bool) Availability {
	if inStock {
		return AvailabilityInStock
	}
	return AvailabilityOutOfStock
}

type ScrapedSet struct {
	ExternalID string
	URL        string
	Name       string
	Grade      string
	PriceCents int
	Currency   string
	// Availability is the shop's stock status for the set; empty if the
	// shop doesn't report one.
	Availability Availability
	// EAN/SKU: canonical product identifiers, kept for a future cross-shop
	// matching feature. Not every shop exposes both, so either may be empty.
	EAN string
	SKU string
}

type Scraper interface {
	ShopSlug() string
	ShopName() string
	BaseURL() string
	FetchAll(ctx context.Context) ([]ScrapedSet, error)
}

// BarcodeLookup is implemented by scrapers whose listing endpoint leaves
// out barcodes that a per-product endpoint has. The collector calls it
// only for sets with no known EAN, a capped number per run.
type BarcodeLookup interface {
	// LookupEAN returns set's barcode, or "" if the shop has none for it.
	LookupEAN(ctx context.Context, set ScrapedSet) (string, error)
}

var registry = map[string]Scraper{}

// Register adds a scraper implementation, keyed by its shop slug.
func Register(s Scraper) {
	registry[s.ShopSlug()] = s
}

// Get looks up a registered scraper by shop slug.
func Get(slug string) (Scraper, bool) {
	s, ok := registry[slug]
	return s, ok
}

// All returns every registered scraper, sorted by slug (registry is a map,
// so iteration order would otherwise vary run to run).
func All() []Scraper {
	all := make([]Scraper, 0, len(registry))
	for _, s := range registry {
		all = append(all, s)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ShopSlug() < all[j].ShopSlug() })
	return all
}
