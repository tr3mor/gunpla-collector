package store

import (
	"context"
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
	// Active is false for a set its shop no longer lists.
	Active bool
	// ProductID is the product the set is currently in; 0 if none yet.
	ProductID int64
	// Method is how the set got into its product, as the product module
	// recorded it; empty if it is alone or was never matched.
	Method string
}

// Listings loads every set, active or not (history keeps its grouping).
func (s *Store) Listings(ctx context.Context) ([]ListingRow, error) {
	return listings(ctx, s.conn)
}

func listings(ctx context.Context, q dbtx) ([]ListingRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT s.id, sh.slug, s.name, COALESCE(s.grade, ''), COALESCE(s.ean, ''), s.is_active,
		        COALESCE(s.product_id, 0), COALESCE(s.match_method, '')
		 FROM sets s JOIN shops sh ON sh.id = s.shop_id
		 ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("query listings: %w", err)
	}
	defer rows.Close()
	var out []ListingRow
	for rows.Next() {
		var l ListingRow
		if err := rows.Scan(&l.ID, &l.Shop, &l.Name, &l.Grade, &l.EAN, &l.Active, &l.ProductID, &l.Method); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ProductTx is the product persistence inside one transaction. It only
// reads and writes rows; which set belongs where is the caller's decision.
type ProductTx struct {
	conn dbtx
}

// InProductTx runs fn in a transaction and commits if it returns nil, so
// a decision made from what fn read can't be overtaken by another write.
func (s *Store) InProductTx(ctx context.Context, fn func(*ProductTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op once committed
	if err := fn(&ProductTx{conn: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// Listings loads every set, active or not.
func (t *ProductTx) Listings(ctx context.Context) ([]ListingRow, error) {
	return listings(ctx, t.conn)
}

// CreateProduct inserts a product and returns its id.
func (t *ProductTx) CreateProduct(ctx context.Context, name, grade, now string) (int64, error) {
	res, err := t.conn.ExecContext(ctx, `INSERT INTO products (name, grade, created_at) VALUES (?, ?, ?)`, name, nullIfEmpty(grade), now)
	if err != nil {
		return 0, fmt.Errorf("insert product: %w", err)
	}
	return res.LastInsertId()
}

// UpdateProduct sets a product's display name and grade.
func (t *ProductTx) UpdateProduct(ctx context.Context, id int64, name, grade string) error {
	if _, err := t.conn.ExecContext(ctx, `UPDATE products SET name = ?, grade = ? WHERE id = ?`, name, nullIfEmpty(grade), id); err != nil {
		return fmt.Errorf("update product: %w", err)
	}
	return nil
}

// AssignSet puts a set in a product, recording how it got there (empty for
// a set that is alone).
func (t *ProductTx) AssignSet(ctx context.Context, setID, productID int64, method string) error {
	if _, err := t.conn.ExecContext(ctx, `UPDATE sets SET product_id = ?, match_method = ? WHERE id = ?`, productID, nullIfEmpty(method), setID); err != nil {
		return fmt.Errorf("update set %d: %w", setID, err)
	}
	return nil
}

// PruneProducts deletes products left with no sets and returns how many.
func (t *ProductTx) PruneProducts(ctx context.Context) (int, error) {
	res, err := t.conn.ExecContext(ctx, `DELETE FROM products WHERE id NOT IN (SELECT product_id FROM sets WHERE product_id IS NOT NULL)`)
	if err != nil {
		return 0, fmt.Errorf("delete empty products: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
