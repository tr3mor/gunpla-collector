// Package catalog owns products: the grouping of one kit's listings across
// shops. It regroups listings after a collect (and on demand), applies
// hand-made links, and serves the read side the search UI needs: filtered
// listings and products with the cheapest one flagged.
package catalog

import (
	"context"
	"sync"

	"gunpla-collector/internal/store"
)

// Store is the persistence the catalog needs; *store.Store satisfies it.
type Store interface {
	Listings(ctx context.Context) ([]store.ListingRow, error)
	InProductTx(ctx context.Context, fn func(*store.ProductTx) error) error
	SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error)

	// Watches and near-miss review.
	Watches(ctx context.Context) ([]store.Watch, error)
	WatchByID(ctx context.Context, id int64) (store.Watch, error)
	CreateWatch(ctx context.Context, w store.Watch, now string) (int64, error)
	UpdateWatch(ctx context.Context, w store.Watch, resetState bool, now string) error
	DeleteWatch(ctx context.Context, id int64) error
	WatchState(ctx context.Context, id int64) (store.WatchState, bool, error)
	SaveWatchState(ctx context.Context, st store.WatchState, now string) error
	DismissedMatches(ctx context.Context) (map[[2]int64]bool, error)
	DismissMatch(ctx context.Context, a, b int64, now string) error
	RestoreMatch(ctx context.Context, a, b int64) error
}

// Catalog groups listings into products and answers queries over them.
type Catalog struct {
	store Store
	// keys caches matcher.NewKey by listing name: normalising is regex-heavy
	// and a watch preview needs the key of every listing.
	keys sync.Map
}

func New(s Store) *Catalog {
	return &Catalog{store: s}
}
