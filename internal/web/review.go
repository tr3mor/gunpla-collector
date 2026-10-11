package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gunpla-collector/internal/catalog"
	"gunpla-collector/internal/matcher"
)

// Defaults for the review list; it can be large, so it is capped.
const (
	defaultReviewLimit = 200
	maxReviewLimit     = 1000
)

// reviewSideJSON is one listing of a near-miss pair, with how the matcher
// read its name.
type reviewSideJSON struct {
	searchResultJSON
	Model    string   `json:"model"`
	Versions []string `json:"versions"`
	Words    []string `json:"words"`
}

type reviewItemJSON struct {
	Kind      string         `json:"kind"`
	Score     float64        `json:"score"`
	Reason    string         `json:"reason"`
	Blocker   string         `json:"blocker,omitempty"`
	Dismissed bool           `json:"dismissed"`
	A         reviewSideJSON `json:"a"`
	B         reviewSideJSON `json:"b"`
	OnlyA     []string       `json:"only_a"`
	OnlyB     []string       `json:"only_b"`
	Shared    []string       `json:"shared"`
}

type reviewJSON struct {
	Items []reviewItemJSON `json:"items"`
	// Total counts the pairs matching the filters before the limit; Counts
	// breaks them down by kind (ignoring the kind filter).
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts"`
}

func (s *Server) reviewSide(row catalog.ReviewRow, a bool) reviewSideJSON {
	r, k := row.RowA, row.KeyA
	if !a {
		r, k = row.RowB, row.KeyB
	}
	return reviewSideJSON{searchResultJSON: s.toResultJSON(r), Model: k.Model, Versions: nonNil(k.Distinct), Words: nonNil(k.Tokens)}
}

// handleReview lists near-miss pairs: ?kind=close|version, ?q= (text in
// either name), ?shop= (either side), ?dismissed=1, ?limit=.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind, shop := q.Get("kind"), strings.TrimSpace(q.Get("shop"))
	text := strings.ToLower(strings.TrimSpace(q.Get("q")))
	limit := defaultReviewLimit
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = min(n, maxReviewLimit)
	}
	rows, err := s.catalog.NearMisses(r.Context(), q.Get("dismissed") == "1")
	if err != nil {
		s.fail(w, err)
		return
	}
	out := reviewJSON{Items: []reviewItemJSON{}, Counts: map[string]int{matcher.KindClose: 0, matcher.KindVersion: 0}}
	for _, row := range rows {
		if shop != "" && row.RowA.ShopSlug != shop && row.RowB.ShopSlug != shop {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(row.A.Name), text) && !strings.Contains(strings.ToLower(row.B.Name), text) {
			continue
		}
		out.Counts[row.Kind]++
		if kind != "" && row.Kind != kind {
			continue
		}
		out.Total++
		if len(out.Items) >= limit {
			continue
		}
		out.Items = append(out.Items, reviewItemJSON{
			Kind: row.Kind, Score: row.Score, Reason: row.Reason, Blocker: row.Blocker, Dismissed: row.Dismissed,
			A: s.reviewSide(row, true), B: s.reviewSide(row, false),
			OnlyA: nonNil(row.OnlyA), OnlyB: nonNil(row.OnlyB), Shared: nonNil(row.Shared),
		})
	}
	s.writeJSON(w, out)
}

type pairRequest struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

// handlePair answers the review actions that take two set ids.
func (s *Server) handlePair(act func(r *http.Request, a, b int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req pairRequest
		if !s.decode(w, r, &req) {
			return
		}
		if req.A <= 0 || req.B <= 0 || req.A == req.B {
			s.writeError(w, http.StatusBadRequest, errors.New("a and b must be two different set ids"))
			return
		}
		if err := act(r, req.A, req.B); err != nil {
			s.fail(w, err)
			return
		}
		s.writeJSON(w, map[string]bool{"ok": true})
	}
}
