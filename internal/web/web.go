// Package web serves the search UI: a single static page (index.html) plus
// a small JSON API it calls into. Read-only — it never writes to the
// database, which stays owned by collect/report.
package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

//go:embed index.html
var indexHTML []byte

// DataStore is the store dependency the web server needs — matches
// *store.Store's method set, kept as an interface so handlers are testable
// against a fake.
type DataStore interface {
	SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error)
	ActiveShops(ctx context.Context) ([]store.Shop, error)
}

// Server serves the search UI and its backing JSON API.
type Server struct {
	store  DataStore
	logger *slog.Logger
}

func NewServer(ds DataStore, logger *slog.Logger) *Server {
	return &Server{store: ds, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/shops", s.handleShops)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(indexHTML); err != nil {
		s.logger.Error("write index response", "error", err)
	}
}

func (s *Server) handleShops(w http.ResponseWriter, r *http.Request) {
	shops, err := s.store.ActiveShops(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}
	type shopJSON struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	out := make([]shopJSON, 0, len(shops))
	for _, sh := range shops {
		out = append(out, shopJSON{Slug: sh.Slug, Name: sh.Name})
	}
	s.writeJSON(w, out)
}

// searchResultJSON is the wire shape for one row in the search results —
// deliberately flat (no nested shop object) to keep the frontend simple.
type searchResultJSON struct {
	ShopSlug     string `json:"shop_slug"`
	ShopName     string `json:"shop_name"`
	Name         string `json:"name"`
	Grade        string `json:"grade"`
	URL          string `json:"url"`
	CurrentCents int    `json:"current_price_cents"`
	Currency     string `json:"currency"`
	Availability string `json:"availability"`
	LowestCents  int    `json:"lowest_price_cents"`
	ScrapedAt    string `json:"scraped_at"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nameSubstr := strings.TrimSpace(q.Get("q"))
	nameRegexp := strings.TrimSpace(q.Get("regexp"))
	shopSlug := strings.TrimSpace(q.Get("shop"))
	// Out-of-stock sets are hidden unless the caller explicitly asks to see
	// them — matches the UI's "hide out of stock" checkbox, checked by
	// default.
	showOutOfStock := q.Get("show_out_of_stock") == "1" || strings.EqualFold(q.Get("show_out_of_stock"), "true")

	var re *regexp.Regexp
	if nameRegexp != "" {
		var err error
		// Case-insensitive by default — (?i) is harmless to prepend even if
		// the caller already wrote their own flags.
		re, err = regexp.Compile("(?i)" + nameRegexp)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
	}

	rows, err := s.store.SearchSets(r.Context(), shopSlug)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err)
		return
	}

	out := make([]searchResultJSON, 0, len(rows))
	for _, row := range rows {
		if nameSubstr != "" && !strings.Contains(strings.ToLower(row.Name), strings.ToLower(nameSubstr)) {
			continue
		}
		if re != nil && !re.MatchString(row.Name) {
			continue
		}
		if !showOutOfStock && row.Availability == scraper.AvailabilityOutOfStock {
			continue
		}
		out = append(out, searchResultJSON{
			ShopSlug:     row.ShopSlug,
			ShopName:     row.ShopName,
			Name:         row.Name,
			Grade:        row.Grade,
			URL:          row.URL,
			CurrentCents: row.CurrentCents,
			Currency:     row.Currency,
			Availability: string(row.Availability),
			LowestCents:  row.LowestCents,
			ScrapedAt:    row.ScrapedAt,
		})
	}
	s.writeJSON(w, out)
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("encode json response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if encErr := json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}); encErr != nil {
		s.logger.Error("encode json error response", "error", encErr)
	}
}
