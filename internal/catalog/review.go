package catalog

import (
	"context"
	"fmt"
	"time"

	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/store"
)

// ReviewRow is one near-miss pair with the listing details the review page
// shows (price, link, stock).
type ReviewRow struct {
	matcher.ReviewItem
	RowA, RowB store.SetSearchRow
	Dismissed  bool
}

// NearMisses lists cross-shop pairs of active listings that were not
// grouped but might be the same kit, best first. It only reads: unlike
// Regroup it never changes products. Dismissed pairs are left out unless
// includeDismissed.
func (c *Catalog) NearMisses(ctx context.Context, includeDismissed bool) ([]ReviewRow, error) {
	all, err := c.store.Listings(ctx)
	if err != nil {
		return nil, err
	}
	active, err := c.store.SearchSets(ctx, "")
	if err != nil {
		return nil, err
	}
	rowByID := make(map[int64]store.SetSearchRow, len(active))
	for _, r := range active {
		rowByID[r.SetID] = r
	}
	listings := activeListings(all) // inactive sets can't be bought
	groups, _ := matcher.Match(listings)

	dismissed, err := c.store.DismissedMatches(ctx)
	if err != nil {
		return nil, err
	}
	var out []ReviewRow
	for _, it := range matcher.Review(listings, groups) {
		a, b := it.A.ID, it.B.ID
		if a > b {
			a, b = b, a
		}
		d := dismissed[[2]int64{a, b}]
		if d && !includeDismissed {
			continue
		}
		out = append(out, ReviewRow{ReviewItem: it, RowA: rowByID[it.A.ID], RowB: rowByID[it.B.ID], Dismissed: d})
	}
	return out, nil
}

// DismissMatch hides a reviewed pair from NearMisses.
func (c *Catalog) DismissMatch(ctx context.Context, a, b int64) error {
	if a == b || a <= 0 || b <= 0 {
		return fmt.Errorf("need two different set ids")
	}
	return c.store.DismissMatch(ctx, a, b, time.Now().UTC().Format(time.RFC3339))
}

// RestoreMatch un-hides a dismissed pair.
func (c *Catalog) RestoreMatch(ctx context.Context, a, b int64) error {
	return c.store.RestoreMatch(ctx, a, b)
}
