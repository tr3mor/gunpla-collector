// Package watchlist owns watches: saved alerts on a kit's price or stock. It
// resolves a watch's spec against the active listings, decides which watches
// have news (Evaluate), and stores the alert state that keeps a standing
// condition from being repeated.
//
// It works on its own Listing type, built from the store's search rows at the
// edge, so it doesn't depend on how products are grouped or how the search
// UI reads prices.
package watchlist

import (
	"context"
	"sync"

	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// Store is the persistence the watchlist needs; *store.Store satisfies it.
type Store interface {
	SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error)

	Watches(ctx context.Context) ([]store.Watch, error)
	WatchByID(ctx context.Context, id int64) (store.Watch, error)
	CreateWatch(ctx context.Context, w store.Watch, now string) (int64, error)
	UpdateWatch(ctx context.Context, w store.Watch, resetState bool, now string) error
	DeleteWatch(ctx context.Context, id int64) error
	WatchState(ctx context.Context, id int64) (store.WatchState, bool, error)
	SaveWatchState(ctx context.Context, st store.WatchState, now string) error
}

// Watchlist resolves and evaluates watches.
type Watchlist struct {
	store Store
	// keys caches matcher.NewKey by listing name: normalising is regex-heavy
	// and a preview needs the key of every listing.
	keys sync.Map
}

func New(s Store) *Watchlist {
	return &Watchlist{store: s}
}

// Listing is an active listing as the watchlist sees it. Prices are the
// shop's own plus their EUR value; HasEUR is false for a currency that had no
// rate, and such a listing never counts as buyable or best.
type Listing struct {
	SetID       int64
	ShopSlug    string
	ShopName    string
	ProductName string
	Name        string
	Grade       string
	URL         string

	CurrentCents int
	Currency     string
	Availability scraper.Availability
	LowestCents  int

	EURCents       int
	LowestEURCents int
	HasEUR         bool

	ScrapedAt string
}

func listingFromRow(r store.SetSearchRow) Listing {
	return Listing{
		SetID: r.SetID, ProductName: r.ProductName, ShopSlug: r.ShopSlug, ShopName: r.ShopName, Name: r.Name, Grade: r.Grade, URL: r.URL,
		CurrentCents: r.CurrentCents, Currency: r.Currency, Availability: r.Availability, LowestCents: r.LowestCents,
		EURCents: r.EURCents, LowestEURCents: r.LowestEURCents, HasEUR: r.HasEUR, ScrapedAt: r.ScrapedAt,
	}
}

// buyableEUR is the listing's price in EUR if it can be bought now and has
// one.
func buyableEUR(l Listing) (int, bool) {
	if l.Availability == scraper.AvailabilityOutOfStock || !l.HasEUR {
		return 0, false
	}
	return l.EURCents, true
}

// snapshot is the active listings with matcher keys, loaded once so many
// watches can be resolved without re-reading or re-normalising them.
type snapshot struct {
	rows []Listing
	keys []matcher.Key
}

func (c *Watchlist) snapshot(ctx context.Context) (*snapshot, error) {
	rows, err := c.store.SearchSets(ctx, "")
	if err != nil {
		return nil, err
	}
	snap := &snapshot{rows: make([]Listing, len(rows)), keys: make([]matcher.Key, len(rows))}
	for i, r := range rows {
		snap.rows[i] = listingFromRow(r)
		snap.keys[i] = c.key(r.Name)
	}
	return snap, nil
}

func (c *Watchlist) key(name string) matcher.Key {
	if k, ok := c.keys.Load(name); ok {
		return k.(matcher.Key)
	}
	k := matcher.NewKey(name)
	c.keys.Store(name, k)
	return k
}
