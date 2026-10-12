package store

import (
	"context"
	"fmt"
)

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
