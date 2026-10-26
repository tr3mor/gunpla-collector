package catalog

import (
	"context"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// Series is the price history of one listing, one entry per successful run
// of its shop since the listing was first recorded.
type Series struct {
	SetID    int64
	ShopSlug string
	ShopName string
	Name     string
	URL      string
	// Active is false once the shop no longer lists the listing.
	Active  bool
	Entries []Entry
}

// Entry is a run of the listing's shop: a priced point, or a run in which
// the shop did not list the listing.
type Entry struct {
	// At is when the record was scraped, or when the run started for an
	// entry that is not listed.
	At     string
	Listed bool

	// The rest is set only for a listed entry.
	Availability scraper.Availability
	// EURCents is the price in EUR. Cents/Currency are the shop's own price.
	EURCents int
	Cents    int
	Currency string
}

// History returns the price history of the listings of one product, or of
// one listing when productID is 0. It returns store.ErrKitNotFound when
// nothing matches.
func (c *Catalog) History(ctx context.Context, productID, setID int64) ([]Series, error) {
	hs, err := c.store.History(ctx, productID, setID)
	if err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(hs))
	for _, h := range hs {
		out = append(out, Series{
			SetID: h.SetID, ShopSlug: h.ShopSlug, ShopName: h.ShopName,
			Name: h.Name, URL: h.URL, Active: h.Active,
			Entries: entries(h),
		})
	}
	return out, nil
}

// entries walks the shop's successful runs from the listing's first record:
// a run with a record is a point (left out when the record has no EUR value,
// since it cannot go on a EUR axis), a run without one is "not listed". Runs
// that failed never reach h.Runs, so they leave no gap.
func entries(h store.SetHistory) []Entry {
	if len(h.Records) == 0 {
		return nil
	}
	byRun := make(map[int64]store.HistoryRecord, len(h.Records))
	for _, r := range h.Records {
		byRun[r.RunID] = r
	}
	first := h.Records[0].RunID

	var out []Entry
	for _, run := range h.Runs {
		if run.ID < first {
			continue
		}
		rec, ok := byRun[run.ID]
		switch {
		case !ok:
			out = append(out, Entry{At: run.StartedAt})
		case rec.HasEUR:
			out = append(out, Entry{
				At: rec.ScrapedAt, Listed: true, Availability: rec.Availability,
				EURCents: rec.EURCents, Cents: rec.PriceCents, Currency: rec.Currency,
			})
		}
	}
	return out
}
