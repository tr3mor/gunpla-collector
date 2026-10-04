package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"gunpla-collector/internal/matcher"
)

// MatchListings loads every set (active or not, so history keeps its
// grouping) in the shape the matcher wants.
func (s *Store) MatchListings(ctx context.Context) ([]matcher.Listing, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT s.id, sh.slug, s.name, COALESCE(s.grade, ''), COALESCE(s.ean, ''),
		        COALESCE(s.product_id, 0), COALESCE(s.match_method, '') = 'manual'
		 FROM sets s JOIN shops sh ON sh.id = s.shop_id
		 ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("query listings: %w", err)
	}
	defer rows.Close()
	var out []matcher.Listing
	for rows.Next() {
		var l matcher.Listing
		if err := rows.Scan(&l.ID, &l.Shop, &l.Name, &l.Grade, &l.EAN, &l.ProductID, &l.Manual); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// MatchStats summarises one ApplyMatches call.
type MatchStats struct {
	Products     int // products after the run
	MultiShop    int // products listed by two or more shops
	Created      int // new product rows
	Removed      int // product rows left with no listings
	Reassigned   int // listings whose product_id changed
	ByMethod     map[string]int
	ListingCount int
}

// ApplyMatches stores groups as products in one transaction. A group keeps
// the product id its members already had (lowest wins; a split gets fresh
// ids), so ids stay stable between runs. Products left with no listings are
// deleted.
func (s *Store) ApplyMatches(ctx context.Context, listings []matcher.Listing, groups []matcher.Group, now string) (MatchStats, error) {
	byID := make(map[int64]matcher.Listing, len(listings))
	for _, l := range listings {
		byID[l.ID] = l
	}
	stats := MatchStats{ByMethod: map[string]int{}, ListingCount: len(listings)}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op once committed
	c := &Store{db: s.db, conn: tx}

	used := map[int64]bool{}
	for _, g := range groups {
		// Pick the product id to keep: the lowest one a member already has
		// that no earlier group claimed.
		var pid int64
		for _, id := range g.Members {
			if p := byID[id].ProductID; p != 0 && !used[p] && (pid == 0 || p < pid) {
				pid = p
			}
		}
		name, grade := canonical(g, byID)
		if pid == 0 {
			res, err := c.conn.ExecContext(ctx, `INSERT INTO products (name, grade, created_at) VALUES (?, ?, ?)`, name, nullIfEmpty(grade), now)
			if err != nil {
				return stats, fmt.Errorf("insert product: %w", err)
			}
			if pid, err = res.LastInsertId(); err != nil {
				return stats, err
			}
			stats.Created++
		} else if _, err := c.conn.ExecContext(ctx, `UPDATE products SET name = ?, grade = ? WHERE id = ?`, name, nullIfEmpty(grade), pid); err != nil {
			return stats, fmt.Errorf("update product: %w", err)
		}
		used[pid] = true

		shops := map[string]bool{}
		for _, id := range g.Members {
			l := byID[id]
			shops[l.Shop] = true
			if l.ProductID != pid {
				stats.Reassigned++
			}
			stats.ByMethod[methodLabel(g.Method[id])]++
			if _, err := c.conn.ExecContext(ctx, `UPDATE sets SET product_id = ?, match_method = ? WHERE id = ?`,
				pid, nullIfEmpty(g.Method[id]), id); err != nil {
				return stats, fmt.Errorf("update set %d: %w", id, err)
			}
		}
		stats.Products++
		if len(shops) > 1 {
			stats.MultiShop++
		}
	}

	res, err := c.conn.ExecContext(ctx, `DELETE FROM products WHERE id NOT IN (SELECT product_id FROM sets WHERE product_id IS NOT NULL)`)
	if err != nil {
		return stats, fmt.Errorf("delete empty products: %w", err)
	}
	n, _ := res.RowsAffected()
	stats.Removed = int(n)
	return stats, tx.Commit()
}

func methodLabel(m string) string {
	if m == "" {
		return "single"
	}
	return m
}

// canonical picks a group's display name (the shortest listing name — shops
// pad names with "Bandai Model Kit" style noise) and grade (first non-empty).
func canonical(g matcher.Group, byID map[int64]matcher.Listing) (name, grade string) {
	ids := append([]int64(nil), g.Members...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		l := byID[id]
		if name == "" || len(l.Name) < len(name) {
			name = l.Name
		}
		if grade == "" {
			grade = l.Grade
		}
	}
	return name, grade
}

// LinkSets puts set b into set a's product by hand (creating one for a if
// it has none). Both become 'manual', so automatic matching leaves them
// alone from then on.
func (s *Store) LinkSets(ctx context.Context, a, b int64) error {
	var pid sql.NullInt64
	if err := s.conn.QueryRowContext(ctx, `SELECT product_id FROM sets WHERE id = ?`, a).Scan(&pid); err != nil {
		return fmt.Errorf("set %d: %w", a, err)
	}
	if err := s.conn.QueryRowContext(ctx, `SELECT 1 FROM sets WHERE id = ?`, b).Scan(new(int)); err != nil {
		return fmt.Errorf("set %d: %w", b, err)
	}
	if !pid.Valid {
		return fmt.Errorf("set %d has no product yet — run `match` first", a)
	}
	if _, err := s.conn.ExecContext(ctx, `UPDATE sets SET product_id = ?, match_method = 'manual' WHERE id IN (?, ?)`, pid.Int64, a, b); err != nil {
		return fmt.Errorf("link sets: %w", err)
	}
	return s.pruneProducts(ctx)
}

// UnlinkSet moves a set into a product of its own and pins it there.
func (s *Store) UnlinkSet(ctx context.Context, id int64, now string) error {
	var name string
	var grade sql.NullString
	if err := s.conn.QueryRowContext(ctx, `SELECT name, grade FROM sets WHERE id = ?`, id).Scan(&name, &grade); err != nil {
		return fmt.Errorf("set %d: %w", id, err)
	}
	res, err := s.conn.ExecContext(ctx, `INSERT INTO products (name, grade, created_at) VALUES (?, ?, ?)`, name, grade, now)
	if err != nil {
		return fmt.Errorf("insert product: %w", err)
	}
	pid, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := s.conn.ExecContext(ctx, `UPDATE sets SET product_id = ?, match_method = 'manual' WHERE id = ?`, pid, id); err != nil {
		return fmt.Errorf("unlink set: %w", err)
	}
	return s.pruneProducts(ctx)
}

func (s *Store) pruneProducts(ctx context.Context) error {
	_, err := s.conn.ExecContext(ctx, `DELETE FROM products WHERE id NOT IN (SELECT product_id FROM sets WHERE product_id IS NOT NULL)`)
	return err
}
