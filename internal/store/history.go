package store

import (
	"context"
	"errors"
	"fmt"

	"gunpla-collector/internal/scraper"
)

// ErrKitNotFound is returned by History when the product has no listings or
// the listing does not exist.
var ErrKitNotFound = errors.New("kit not found")

// HistoryRecord is one price_history row of a listing.
type HistoryRecord struct {
	RunID        int64
	ScrapedAt    string
	PriceCents   int
	Currency     string
	Availability scraper.Availability
	// EURCents is the price converted when it was collected; HasEUR is false
	// for a currency that had no rate.
	EURCents int
	HasEUR   bool
}

// HistoryRun is a successful run of a listing's shop.
type HistoryRun struct {
	ID        int64
	StartedAt string
}

// SetHistory is one listing with every price record it has, and the
// successful runs of its shop — the runs are what tell "the shop did not
// list it" apart from "nothing was collected".
type SetHistory struct {
	SetID    int64
	ShopSlug string
	ShopName string
	Name     string
	URL      string
	Active   bool
	// Records are ordered by run, oldest first.
	Records []HistoryRecord
	// Runs are the shop's successful runs, oldest first.
	Runs []HistoryRun
}

// History returns the price history of the listings of one product, or of
// one listing when productID is 0. Listings come ordered by shop name, then
// listing name, and include listings their shop no longer lists. It returns
// ErrKitNotFound when nothing matches.
func (s *Store) History(ctx context.Context, productID, setID int64) ([]SetHistory, error) {
	query := `SELECT s.id, sh.id, sh.slug, sh.name, s.name, s.url, s.is_active
		FROM sets s JOIN shops sh ON sh.id = s.shop_id
		WHERE `
	arg := setID
	if productID != 0 {
		query += `s.product_id = ?`
		arg = productID
	} else {
		query += `s.id = ?`
	}
	query += ` ORDER BY sh.name, s.name, s.id`

	rows, err := s.conn.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("query history listings: %w", err)
	}
	defer rows.Close()

	var out []SetHistory
	var shopIDs []int64
	for rows.Next() {
		var h SetHistory
		var shopID int64
		var active int
		if err := rows.Scan(&h.SetID, &shopID, &h.ShopSlug, &h.ShopName, &h.Name, &h.URL, &active); err != nil {
			return nil, err
		}
		h.Active = active != 0
		out = append(out, h)
		shopIDs = append(shopIDs, shopID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrKitNotFound
	}

	runsByShop := map[int64][]HistoryRun{}
	for i := range out {
		runs, ok := runsByShop[shopIDs[i]]
		if !ok {
			if runs, err = s.successfulRuns(ctx, shopIDs[i]); err != nil {
				return nil, err
			}
			runsByShop[shopIDs[i]] = runs
		}
		out[i].Runs = runs
		if out[i].Records, err = s.historyRecords(ctx, out[i].SetID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) successfulRuns(ctx context.Context, shopID int64) ([]HistoryRun, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT id, started_at FROM scrape_runs WHERE shop_id = ? AND status = 'success' ORDER BY id`, shopID)
	if err != nil {
		return nil, fmt.Errorf("query successful runs: %w", err)
	}
	defer rows.Close()
	var runs []HistoryRun
	for rows.Next() {
		var r HistoryRun
		if err := rows.Scan(&r.ID, &r.StartedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *Store) historyRecords(ctx context.Context, setID int64) ([]HistoryRecord, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT run_id, scraped_at, price_cents, currency, COALESCE(availability, ''), COALESCE(eur_cents, 0), eur_cents IS NOT NULL
		 FROM price_history WHERE set_id = ? ORDER BY run_id`, setID)
	if err != nil {
		return nil, fmt.Errorf("query price history: %w", err)
	}
	defer rows.Close()
	var recs []HistoryRecord
	for rows.Next() {
		var r HistoryRecord
		if err := rows.Scan(&r.RunID, &r.ScrapedAt, &r.PriceCents, &r.Currency, &r.Availability, &r.EURCents, &r.HasEUR); err != nil {
			return nil, err
		}
		recs = append(recs, r)
	}
	return recs, rows.Err()
}
