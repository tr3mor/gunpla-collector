// Package store provides SQLite-backed persistence for shops, sets, price
// history, and scrape runs.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"gunpla-collector/internal/scraper"
)

//go:embed schema.sql
var schemaSQL string

// dbtx is satisfied by both *sql.DB and *sql.Tx, so query methods below can
// run either directly against the pool or scoped to a transaction.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db   *sql.DB
	conn dbtx
}

type Shop struct {
	ID      int64
	Slug    string
	Name    string
	BaseURL string
	Active  bool
}

type Run struct {
	ID         int64
	ShopID     int64
	StartedAt  string
	FinishedAt sql.NullString
	Status     string
	SetsFound  sql.NullInt64
	Error      sql.NullString
}

// ReportItem is a set + the price it had at some run, used for "new" and
// "removed" report sections.
type ReportItem struct {
	Name       string
	Grade      string
	PriceCents int
	Currency   string
}

// PriceChange is a set whose price differs between two runs.
type PriceChange struct {
	Name     string
	Grade    string
	OldCents int
	NewCents int
	Currency string
}

func Open(path string) (*Store, error) {
	// _foreign_keys=on is set via the DSN (not a one-off PRAGMA exec) because
	// PRAGMA state is per-connection: database/sql can transparently open a
	// new underlying connection later, and a one-off PRAGMA on the first
	// connection wouldn't carry over to it. The driver applies DSN pragmas
	// to every connection it opens.
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // avoid SQLITE_BUSY: single-writer file, cron-driven, not a concurrent service
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db, conn: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// GetOrCreateShop ensures a shop row exists for slug, creating it (active)
// if missing. Existing rows are left untouched.
func (s *Store) GetOrCreateShop(ctx context.Context, slug, name, baseURL string) (Shop, error) {
	shop, ok, err := s.ShopBySlug(ctx, slug)
	if err != nil {
		return Shop{}, err
	}
	if ok {
		return shop, nil
	}
	res, err := s.conn.ExecContext(ctx,
		`INSERT INTO shops (slug, name, base_url, active) VALUES (?, ?, ?, 1)`,
		slug, name, baseURL)
	if err != nil {
		return Shop{}, fmt.Errorf("insert shop: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Shop{}, err
	}
	return Shop{ID: id, Slug: slug, Name: name, BaseURL: baseURL, Active: true}, nil
}

func (s *Store) ShopBySlug(ctx context.Context, slug string) (Shop, bool, error) {
	row := s.conn.QueryRowContext(ctx,
		`SELECT id, slug, name, base_url, active FROM shops WHERE slug = ?`, slug)
	var sh Shop
	var active int
	if err := row.Scan(&sh.ID, &sh.Slug, &sh.Name, &sh.BaseURL, &active); err != nil {
		if err == sql.ErrNoRows {
			return Shop{}, false, nil
		}
		return Shop{}, false, fmt.Errorf("query shop: %w", err)
	}
	sh.Active = active != 0
	return sh, true, nil
}

func (s *Store) ActiveShops(ctx context.Context) ([]Shop, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT id, slug, name, base_url, active FROM shops WHERE active = 1`)
	if err != nil {
		return nil, fmt.Errorf("query active shops: %w", err)
	}
	defer rows.Close()
	var shops []Shop
	for rows.Next() {
		var sh Shop
		var active int
		if err := rows.Scan(&sh.ID, &sh.Slug, &sh.Name, &sh.BaseURL, &active); err != nil {
			return nil, err
		}
		sh.Active = active != 0
		shops = append(shops, sh)
	}
	return shops, rows.Err()
}

// StartRun records a new in-progress scrape run.
func (s *Store) StartRun(ctx context.Context, shopID int64, startedAt string) (int64, error) {
	res, err := s.conn.ExecContext(ctx,
		`INSERT INTO scrape_runs (shop_id, started_at, status) VALUES (?, ?, 'running')`,
		shopID, startedAt)
	if err != nil {
		return 0, fmt.Errorf("insert scrape_run: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) FinishRunSuccess(ctx context.Context, runID int64, finishedAt string, setsFound int) error {
	_, err := s.conn.ExecContext(ctx,
		`UPDATE scrape_runs SET finished_at = ?, status = 'success', sets_found = ? WHERE id = ?`,
		finishedAt, setsFound, runID)
	return err
}

func (s *Store) FinishRunFailed(ctx context.Context, runID int64, finishedAt string, errMsg string) error {
	_, err := s.conn.ExecContext(ctx,
		`UPDATE scrape_runs SET finished_at = ?, status = 'failed', error = ? WHERE id = ?`,
		finishedAt, errMsg, runID)
	return err
}

// LastSuccessfulRunSetsFound returns sets_found from the most recent
// successful run for the shop, used as a sanity baseline for the current run.
func (s *Store) LastSuccessfulRunSetsFound(ctx context.Context, shopID int64) (int, bool, error) {
	row := s.conn.QueryRowContext(ctx,
		`SELECT sets_found FROM scrape_runs
		 WHERE shop_id = ? AND status = 'success' AND sets_found IS NOT NULL
		 ORDER BY id DESC LIMIT 1`, shopID)
	var count int
	if err := row.Scan(&count); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("query last successful run: %w", err)
	}
	return count, true, nil
}

// UpsertSet inserts a new set or updates an existing one (matched on
// shop_id+external_id), reactivating it if it had previously been marked
// removed. Returns the set's id.
func (s *Store) UpsertSet(ctx context.Context, shopID int64, externalID, url, name, grade, now string) (int64, error) {
	row := s.conn.QueryRowContext(ctx,
		`SELECT id FROM sets WHERE shop_id = ? AND external_id = ?`, shopID, externalID)
	var id int64
	err := row.Scan(&id)
	switch {
	case err == sql.ErrNoRows:
		res, err := s.conn.ExecContext(ctx,
			`INSERT INTO sets (shop_id, external_id, url, name, grade, first_seen_at, last_seen_at, is_active, removed_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1, NULL)`,
			shopID, externalID, url, name, grade, now, now)
		if err != nil {
			return 0, fmt.Errorf("insert set: %w", err)
		}
		return res.LastInsertId()
	case err != nil:
		return 0, fmt.Errorf("query set: %w", err)
	default:
		_, err = s.conn.ExecContext(ctx,
			`UPDATE sets SET url = ?, name = ?, grade = ?, last_seen_at = ?, is_active = 1, removed_at = NULL
			 WHERE id = ?`,
			url, name, grade, now, id)
		if err != nil {
			return 0, fmt.Errorf("update set: %w", err)
		}
		return id, nil
	}
}

func (s *Store) InsertPriceHistory(ctx context.Context, setID, runID int64, priceCents int, currency string, inStock *bool, scrapedAt string) error {
	var inStockVal any
	if inStock != nil {
		if *inStock {
			inStockVal = 1
		} else {
			inStockVal = 0
		}
	}
	_, err := s.conn.ExecContext(ctx,
		`INSERT INTO price_history (set_id, run_id, price_cents, currency, in_stock, scraped_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		setID, runID, priceCents, currency, inStockVal, scrapedAt)
	return err
}

// ApplyRun persists a completed scrape's results as a single transaction:
// upserts every set, records its price history, deactivates sets no longer
// seen, and marks the run successful. All-or-nothing, so a crash or
// cancellation mid-run rolls back cleanly instead of leaving sets, price
// history, and the run's status inconsistent with each other.
func (s *Store) ApplyRun(ctx context.Context, shopID, runID int64, sets []scraper.ScrapedSet, now string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	txStore := &Store{db: s.db, conn: tx}

	seenIDs := make([]string, 0, len(sets))
	for _, sc := range sets {
		setID, err := txStore.UpsertSet(ctx, shopID, sc.ExternalID, sc.URL, sc.Name, sc.Grade, now)
		if err != nil {
			return fmt.Errorf("upsert set %s: %w", sc.ExternalID, err)
		}
		if err := txStore.InsertPriceHistory(ctx, setID, runID, sc.PriceCents, sc.Currency, sc.InStock, now); err != nil {
			return fmt.Errorf("insert price history for set %s: %w", sc.ExternalID, err)
		}
		seenIDs = append(seenIDs, sc.ExternalID)
	}

	if err := txStore.DeactivateMissing(ctx, shopID, seenIDs, now); err != nil {
		return fmt.Errorf("deactivate missing sets: %w", err)
	}

	if err := txStore.FinishRunSuccess(ctx, runID, now, len(sets)); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	return tx.Commit()
}

// DeactivateMissing marks is_active=0/removed_at=now for every active set of
// this shop whose external_id was not seen in the current run.
func (s *Store) DeactivateMissing(ctx context.Context, shopID int64, seenExternalIDs []string, now string) error {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT id, external_id FROM sets WHERE shop_id = ? AND is_active = 1`, shopID)
	if err != nil {
		return fmt.Errorf("query active sets: %w", err)
	}
	seen := make(map[string]bool, len(seenExternalIDs))
	for _, id := range seenExternalIDs {
		seen[id] = true
	}
	var toRemove []int64
	for rows.Next() {
		var id int64
		var extID string
		if err := rows.Scan(&id, &extID); err != nil {
			rows.Close()
			return err
		}
		if !seen[extID] {
			toRemove = append(toRemove, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	if len(toRemove) == 0 {
		return nil
	}

	placeholders := make([]string, len(toRemove))
	args := make([]any, 0, len(toRemove)+1)
	args = append(args, now)
	for i, id := range toRemove {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `UPDATE sets SET is_active = 0, removed_at = ? WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	if _, err := s.conn.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("deactivate missing sets: %w", err)
	}
	return nil
}

// TwoMostRecentSuccessfulRuns returns the current (most recent) and previous
// successful runs for a shop. previous is nil if there's only one (or zero).
func (s *Store) TwoMostRecentSuccessfulRuns(ctx context.Context, shopID int64) (current *Run, previous *Run, err error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT id, shop_id, started_at, finished_at, status, sets_found, error
		 FROM scrape_runs WHERE shop_id = ? AND status = 'success'
		 ORDER BY id DESC LIMIT 2`, shopID)
	if err != nil {
		return nil, nil, fmt.Errorf("query recent runs: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.ShopID, &r.StartedAt, &r.FinishedAt, &r.Status, &r.SetsFound, &r.Error); err != nil {
			return nil, nil, err
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(runs) == 0 {
		return nil, nil, nil
	}
	current = &runs[0]
	if len(runs) > 1 {
		previous = &runs[1]
	}
	return current, previous, nil
}

// NewSets returns sets that have price_history in currentRunID but not in
// previousRunID (covers both brand-new sets and reactivated ones).
func (s *Store) NewSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]ReportItem, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT s.name, COALESCE(s.grade, ''), ph.price_cents, ph.currency
		 FROM sets s
		 JOIN price_history ph ON ph.set_id = s.id AND ph.run_id = ?
		 WHERE s.shop_id = ?
		   AND NOT EXISTS (SELECT 1 FROM price_history ph2 WHERE ph2.set_id = s.id AND ph2.run_id = ?)
		 ORDER BY s.name`,
		currentRunID, shopID, previousRunID)
	if err != nil {
		return nil, fmt.Errorf("query new sets: %w", err)
	}
	return scanReportItems(rows)
}

// RemovedSets returns sets that had price_history in previousRunID but not
// in currentRunID.
func (s *Store) RemovedSets(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]ReportItem, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT s.name, COALESCE(s.grade, ''), ph.price_cents, ph.currency
		 FROM sets s
		 JOIN price_history ph ON ph.set_id = s.id AND ph.run_id = ?
		 WHERE s.shop_id = ?
		   AND NOT EXISTS (SELECT 1 FROM price_history ph2 WHERE ph2.set_id = s.id AND ph2.run_id = ?)
		 ORDER BY s.name`,
		previousRunID, shopID, currentRunID)
	if err != nil {
		return nil, fmt.Errorf("query removed sets: %w", err)
	}
	return scanReportItems(rows)
}

// PriceChanges returns sets present in both runs whose price differs.
func (s *Store) PriceChanges(ctx context.Context, shopID, currentRunID, previousRunID int64) ([]PriceChange, error) {
	rows, err := s.conn.QueryContext(ctx,
		`SELECT s.name, COALESCE(s.grade, ''), phOld.price_cents, phNew.price_cents, phNew.currency
		 FROM sets s
		 JOIN price_history phOld ON phOld.set_id = s.id AND phOld.run_id = ?
		 JOIN price_history phNew ON phNew.set_id = s.id AND phNew.run_id = ?
		 WHERE s.shop_id = ? AND phOld.price_cents != phNew.price_cents
		 ORDER BY s.name`,
		previousRunID, currentRunID, shopID)
	if err != nil {
		return nil, fmt.Errorf("query price changes: %w", err)
	}
	defer rows.Close()
	var changes []PriceChange
	for rows.Next() {
		var c PriceChange
		if err := rows.Scan(&c.Name, &c.Grade, &c.OldCents, &c.NewCents, &c.Currency); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

func scanReportItems(rows *sql.Rows) ([]ReportItem, error) {
	defer rows.Close()
	var items []ReportItem
	for rows.Next() {
		var it ReportItem
		if err := rows.Scan(&it.Name, &it.Grade, &it.PriceCents, &it.Currency); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
