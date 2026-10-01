package store

import (
	"context"
	"fmt"

	"gunpla-collector/internal/scraper"
)

// SetSearchRow is one (shop, set) pair as shown in the search UI: the set's
// current price (its most recent price_history row) plus the lowest price
// ever recorded for it, regardless of when.
type SetSearchRow struct {
	ShopSlug     string
	ShopName     string
	Name         string
	Grade        string
	URL          string
	CurrentCents int
	Currency     string
	Availability scraper.Availability
	LowestCents  int
	ScrapedAt    string
}

// SearchSets returns every currently-active set (optionally scoped to one
// shop by slug), each with its current and lowest-ever price joined in.
// Name/regexp filtering happens in the caller (internal/web), not here —
// the result set is small enough for a personal catalog that filtering in
// Go avoids needing a SQLite regexp extension.
func (s *Store) SearchSets(ctx context.Context, shopSlug string) ([]SetSearchRow, error) {
	query := `
		SELECT sh.slug, sh.name, s.name, COALESCE(s.grade, ''), s.url,
		       cur.price_cents, cur.currency, COALESCE(cur.availability, ''), cur.scraped_at,
		       low.min_price
		FROM sets s
		JOIN shops sh ON sh.id = s.shop_id
		JOIN (
			SELECT set_id, price_cents, currency, availability, scraped_at
			FROM price_history
			WHERE id IN (SELECT MAX(id) FROM price_history GROUP BY set_id)
		) cur ON cur.set_id = s.id
		JOIN (
			SELECT set_id, MIN(price_cents) AS min_price
			FROM price_history
			GROUP BY set_id
		) low ON low.set_id = s.id
		WHERE s.is_active = 1`
	args := []any{}
	if shopSlug != "" {
		query += ` AND sh.slug = ?`
		args = append(args, shopSlug)
	}
	query += ` ORDER BY s.name, sh.slug`

	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query search sets: %w", err)
	}
	defer rows.Close()

	var results []SetSearchRow
	for rows.Next() {
		var r SetSearchRow
		if err := rows.Scan(&r.ShopSlug, &r.ShopName, &r.Name, &r.Grade, &r.URL,
			&r.CurrentCents, &r.Currency, &r.Availability, &r.ScrapedAt, &r.LowestCents); err != nil {
			return nil, fmt.Errorf("scan search row: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
