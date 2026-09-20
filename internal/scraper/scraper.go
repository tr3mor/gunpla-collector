// Package scraper defines the Scraper interface implemented by each
// supported shop, plus a registry so collector/reporter never need to know
// about individual shop implementations.
package scraper

import (
	"context"
	"sort"
)

type ScrapedSet struct {
	ExternalID string
	URL        string
	Name       string
	Grade      string
	PriceCents int
	Currency   string
	InStock    *bool
	// EAN and SKU are canonical product identifiers, kept for a future
	// cross-shop "same kit at two shops" feature — not every shop exposes
	// both, so either may be empty.
	EAN string
	SKU string
}

type Scraper interface {
	ShopSlug() string
	ShopName() string
	BaseURL() string
	FetchAll(ctx context.Context) ([]ScrapedSet, error)
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

// All returns every registered scraper, sorted by slug — registry is a map,
// so without sorting the order (and therefore request sequencing in
// collect, and shop iteration order in report) would vary run to run.
func All() []Scraper {
	all := make([]Scraper, 0, len(registry))
	for _, s := range registry {
		all = append(all, s)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ShopSlug() < all[j].ShopSlug() })
	return all
}
