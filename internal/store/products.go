package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ListingRow is one stored set as the product module sees it: enough to
// decide which sets are the same kit.
type ListingRow struct {
	ID    int64
	Shop  string // shop slug
	Name  string
	Grade string
	EAN   string
	// ProductID is the product the set is currently in; 0 if none yet.
	ProductID int64
	// Manual marks a set whose product was chosen by hand (link/unlink).
	Manual bool
}

// Listings loads every set (active or not, so history keeps its grouping).
func (s *Store) Listings(ctx context.Context) ([]ListingRow, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT s.id, sh.slug, s.name, COALESCE(s.grade, ''), COALESCE(s.ean, ''),
		        COALESCE(s.product_id, 0), COALESCE(s.match_method, '') = 'manual'
		 FROM sets s JOIN shops sh ON sh.id = s.shop_id
		 ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("query listings: %w", err)
	}
	defer rows.Close()
	var out []ListingRow
	for rows.Next() {
		var l ListingRow
		if err := rows.Scan(&l.ID, &l.Shop, &l.Name, &l.Grade, &l.EAN, &l.ProductID, &l.Manual); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ProductPlan is the desired state of one product: its sets, and how each
// got there.
type ProductPlan struct {
	// ProductID is the existing product to keep; 0 creates a new one.
	ProductID int64
	Name      string
	Grade     string
	Members   []ProductMember
}

// ProductMember assigns one set to a plan, recording how it was matched
// ("ean", "name", "manual"; empty for a set that is alone).
type ProductMember struct {
	SetID  int64
	Method string
}

// ApplyProducts makes the products table match plans in one transaction,
// then deletes products left with no sets. It returns how many products
// it created and deleted.
func (s *Store) ApplyProducts(ctx context.Context, plans []ProductPlan, now string) (created, removed int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	for _, p := range plans {
		pid := p.ProductID
		if pid == 0 {
			res, err := tx.ExecContext(ctx, `INSERT INTO products (name, grade, created_at) VALUES (?, ?, ?)`, p.Name, nullIfEmpty(p.Grade), now)
			if err != nil {
				return 0, 0, fmt.Errorf("insert product: %w", err)
			}
			if pid, err = res.LastInsertId(); err != nil {
				return 0, 0, err
			}
			created++
		} else if _, err := tx.ExecContext(ctx, `UPDATE products SET name = ?, grade = ? WHERE id = ?`, p.Name, nullIfEmpty(p.Grade), pid); err != nil {
			return 0, 0, fmt.Errorf("update product: %w", err)
		}
		for _, m := range p.Members {
			if _, err := tx.ExecContext(ctx, `UPDATE sets SET product_id = ?, match_method = ? WHERE id = ?`,
				pid, nullIfEmpty(m.Method), m.SetID); err != nil {
				return 0, 0, fmt.Errorf("update set %d: %w", m.SetID, err)
			}
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM products WHERE id NOT IN (SELECT product_id FROM sets WHERE product_id IS NOT NULL)`)
	if err != nil {
		return 0, 0, fmt.Errorf("delete empty products: %w", err)
	}
	n, _ := res.RowsAffected()
	return created, int(n), tx.Commit()
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
