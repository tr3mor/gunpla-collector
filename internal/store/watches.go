package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Watch is a saved alert on a kit. Spec is opaque JSON owned by the catalog
// package.
type Watch struct {
	ID    int64
	Label string
	Spec  string
	// MaxPriceCents is the EUR price at or under which to alert; 0 means no
	// price target.
	MaxPriceCents int
	OnRestock     bool
	OnNew         bool
	Enabled       bool
	CreatedAt     string
	UpdatedAt     string
}

// WatchState is what the last alert pass saw for a watch.
type WatchState struct {
	WatchID int64
	// LastAlertCents is the price of the last "target hit" alert, or 0 when
	// the watch is armed (price above target, or never alerted).
	LastAlertCents int
	WasBuyable     bool
	KnownSets      []int64
}

const watchCols = `id, label, spec, max_price_cents, on_restock, on_new, enabled, created_at, updated_at`

func scanWatch(sc interface{ Scan(...any) error }) (Watch, error) {
	var w Watch
	err := sc.Scan(&w.ID, &w.Label, &w.Spec, &w.MaxPriceCents, &w.OnRestock, &w.OnNew, &w.Enabled, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// Watches returns every watch, oldest first.
func (s *Store) Watches(ctx context.Context) ([]Watch, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT `+watchCols+` FROM watches ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query watches: %w", err)
	}
	defer rows.Close()
	var out []Watch
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan watch: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ErrWatchNotFound is returned when a watch id doesn't exist.
var ErrWatchNotFound = errors.New("watch not found")

// WatchByID loads one watch.
func (s *Store) WatchByID(ctx context.Context, id int64) (Watch, error) {
	w, err := scanWatch(s.conn.QueryRowContext(ctx, `SELECT `+watchCols+` FROM watches WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Watch{}, ErrWatchNotFound
	}
	if err != nil {
		return Watch{}, fmt.Errorf("query watch: %w", err)
	}
	return w, nil
}

// CreateWatch inserts w (ignoring w.ID) and returns its id.
func (s *Store) CreateWatch(ctx context.Context, w Watch, now string) (int64, error) {
	res, err := s.conn.ExecContext(ctx,
		`INSERT INTO watches (label, spec, max_price_cents, on_restock, on_new, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		w.Label, w.Spec, w.MaxPriceCents, w.OnRestock, w.OnNew, w.Enabled, now, now)
	if err != nil {
		return 0, fmt.Errorf("insert watch: %w", err)
	}
	return res.LastInsertId()
}

// UpdateWatch overwrites a watch's settings. With resetState, its alert
// state is dropped too, so the next pass starts from a fresh baseline —
// wanted when the spec or price target changed, not for a rename.
func (s *Store) UpdateWatch(ctx context.Context, w Watch, resetState bool, now string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update watch: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE watches SET label = ?, spec = ?, max_price_cents = ?, on_restock = ?, on_new = ?, enabled = ?, updated_at = ?
		 WHERE id = ?`,
		w.Label, w.Spec, w.MaxPriceCents, w.OnRestock, w.OnNew, w.Enabled, now, w.ID)
	if err != nil {
		return fmt.Errorf("update watch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWatchNotFound
	}
	if resetState {
		if _, err := tx.ExecContext(ctx, `DELETE FROM watch_state WHERE watch_id = ?`, w.ID); err != nil {
			return fmt.Errorf("reset watch state: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteWatch removes a watch and (by cascade) its state.
func (s *Store) DeleteWatch(ctx context.Context, id int64) error {
	res, err := s.conn.ExecContext(ctx, `DELETE FROM watches WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrWatchNotFound
	}
	return nil
}

// WatchState loads a watch's alert state; ok is false if it was never
// evaluated.
func (s *Store) WatchState(ctx context.Context, id int64) (st WatchState, ok bool, err error) {
	var known string
	err = s.conn.QueryRowContext(ctx,
		`SELECT watch_id, last_alert_cents, was_buyable, known_sets FROM watch_state WHERE watch_id = ?`, id).
		Scan(&st.WatchID, &st.LastAlertCents, &st.WasBuyable, &known)
	if errors.Is(err, sql.ErrNoRows) {
		return WatchState{}, false, nil
	}
	if err != nil {
		return WatchState{}, false, fmt.Errorf("query watch state: %w", err)
	}
	if err := json.Unmarshal([]byte(known), &st.KnownSets); err != nil {
		return WatchState{}, false, fmt.Errorf("decode known sets: %w", err)
	}
	return st, true, nil
}

// SaveWatchState records the outcome of an alert pass.
func (s *Store) SaveWatchState(ctx context.Context, st WatchState, now string) error {
	known, err := json.Marshal(st.KnownSets)
	if err != nil {
		return err
	}
	if string(known) == "null" {
		known = []byte("[]")
	}
	_, err = s.conn.ExecContext(ctx,
		`INSERT INTO watch_state (watch_id, last_alert_cents, was_buyable, known_sets, evaluated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(watch_id) DO UPDATE SET last_alert_cents = excluded.last_alert_cents,
		   was_buyable = excluded.was_buyable, known_sets = excluded.known_sets, evaluated_at = excluded.evaluated_at`,
		st.WatchID, st.LastAlertCents, st.WasBuyable, string(known), now)
	if err != nil {
		return fmt.Errorf("save watch state: %w", err)
	}
	return nil
}

// DismissMatch hides a near-miss pair from the review page.
func (s *Store) DismissMatch(ctx context.Context, a, b int64, now string) error {
	if a > b {
		a, b = b, a
	}
	_, err := s.conn.ExecContext(ctx,
		`INSERT OR IGNORE INTO match_dismissals (a_id, b_id, created_at) VALUES (?, ?, ?)`, a, b, now)
	if err != nil {
		return fmt.Errorf("dismiss match: %w", err)
	}
	return nil
}

// RestoreMatch un-hides a dismissed pair.
func (s *Store) RestoreMatch(ctx context.Context, a, b int64) error {
	if a > b {
		a, b = b, a
	}
	if _, err := s.conn.ExecContext(ctx, `DELETE FROM match_dismissals WHERE a_id = ? AND b_id = ?`, a, b); err != nil {
		return fmt.Errorf("restore match: %w", err)
	}
	return nil
}

// DismissedMatches returns the dismissed pairs, keyed [a, b] with a < b.
func (s *Store) DismissedMatches(ctx context.Context) (map[[2]int64]bool, error) {
	rows, err := s.conn.QueryContext(ctx, `SELECT a_id, b_id FROM match_dismissals`)
	if err != nil {
		return nil, fmt.Errorf("query dismissals: %w", err)
	}
	defer rows.Close()
	out := map[[2]int64]bool{}
	for rows.Next() {
		var p [2]int64
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}
