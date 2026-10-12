// Package catalog owns products: the grouping of one kit's listings across
// shops. It regroups listings after a collect (and on demand), applies
// hand-made links, and serves the read side the search UI needs: filtered
// listings and products with the cheapest one flagged.
package catalog

import (
	"context"

	"gunpla-collector/internal/store"
)

// Store is the persistence the catalog needs; *store.Store satisfies it.
type Store interface {
	Listings(ctx context.Context) ([]store.ListingRow, error)
	InProductTx(ctx context.Context, fn func(*store.ProductTx) error) error
	SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error)

	// Near-miss review.
	DismissedMatches(ctx context.Context) (map[[2]int64]bool, error)
	DismissMatch(ctx context.Context, a, b int64, now string) error
	RestoreMatch(ctx context.Context, a, b int64) error
}

// Catalog groups listings into products and answers queries over them.
type Catalog struct {
	store Store
}

func New(s Store) *Catalog {
	return &Catalog{store: s}
}
