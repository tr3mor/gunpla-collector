// Package web serves the search UI: a single static page (index.html) plus
// a small JSON API it calls into. Read-only — it never writes to the
// database, which stays owned by collect/report.
package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"gunpla-collector/internal/catalog"
	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/store"
)

//go:embed index.html
var indexHTML []byte

// Catalog is the listings and products the server serves — matches
// *catalog.Catalog, kept as an interface so handlers are testable against
// a fake.
type Catalog interface {
	Display(cents int, currency string) fx.Price
	Search(ctx context.Context, f catalog.Filter) ([]store.SetSearchRow, error)
	Products(ctx context.Context, f catalog.Filter) ([]catalog.Product, error)
	SpecFromSet(ctx context.Context, setID int64) (catalog.WatchSpec, string, error)
	PreviewWatch(ctx context.Context, spec catalog.WatchSpec) (catalog.WatchPreview, error)
	WatchStatuses(ctx context.Context) ([]catalog.WatchStatus, error)
	SaveWatch(ctx context.Context, w catalog.Watch) (catalog.Watch, error)
	DeleteWatch(ctx context.Context, id int64) error
	NearMisses(ctx context.Context, includeDismissed bool) ([]catalog.ReviewRow, error)
	Link(ctx context.Context, a, b int64) error
	DismissMatch(ctx context.Context, a, b int64) error
	RestoreMatch(ctx context.Context, a, b int64) error
}

// Shops lists the shops the UI can filter by — matches *store.Store.
type Shops interface {
	ActiveShops(ctx context.Context) ([]store.Shop, error)
}

// Server serves the search UI and its backing JSON API.
type Server struct {
	catalog Catalog
	shops   Shops
	logger  *slog.Logger
	// writeToken, if set, is required on requests that change data.
	writeToken string
}

// Option configures a Server.
type Option func(*Server)

func NewServer(c Catalog, shops Shops, logger *slog.Logger, opts ...Option) *Server {
	s := &Server{catalog: c, shops: shops, logger: logger}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/shops", s.handleShops)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/products", s.handleProducts)
	mux.HandleFunc("POST /api/watch/preview", s.handleWatchPreview)
	mux.HandleFunc("GET /api/watches", s.handleWatches)
	mux.HandleFunc("POST /api/watches", s.write(s.handleWatchSave))
	mux.HandleFunc("PUT /api/watches/{id}", s.write(s.handleWatchSave))
	mux.HandleFunc("DELETE /api/watches/{id}", s.write(s.handleWatchDelete))
	mux.HandleFunc("GET /api/review", s.handleReview)
	mux.HandleFunc("POST /api/review/link", s.write(s.handlePair(func(r *http.Request, a, b int64) error { return s.catalog.Link(r.Context(), a, b) })))
	mux.HandleFunc("POST /api/review/dismiss", s.write(s.handlePair(func(r *http.Request, a, b int64) error { return s.catalog.DismissMatch(r.Context(), a, b) })))
	mux.HandleFunc("POST /api/review/restore", s.write(s.handlePair(func(r *http.Request, a, b int64) error { return s.catalog.RestoreMatch(r.Context(), a, b) })))
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		s.writeJSON(w, map[string]bool{"write_token_required": s.writeToken != ""})
	})
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(indexHTML); err != nil {
		s.logger.Error("write index response", "error", err)
	}
}

func (s *Server) handleShops(w http.ResponseWriter, r *http.Request) {
	shops, err := s.shops.ActiveShops(r.Context())
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
	SetID        int64  `json:"set_id"`
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
	// OriginalCents/OriginalCurrency hold the shop's own price when it was
	// converted to EUR (Currency is then "EUR"); zero/empty otherwise.
	OriginalCents    int    `json:"original_price_cents,omitempty"`
	OriginalCurrency string `json:"original_currency,omitempty"`
}

// filter reads the request's search filters (q, regexp, shop, grade,
// show_out_of_stock).
func filter(r *http.Request) catalog.Filter {
	q := r.URL.Query()
	return catalog.Filter{
		Query:  strings.TrimSpace(q.Get("q")),
		Regexp: strings.TrimSpace(q.Get("regexp")),
		Shop:   strings.TrimSpace(q.Get("shop")),
		Grade:  strings.TrimSpace(q.Get("grade")),
		// Out-of-stock sets are hidden unless the caller explicitly asks to
		// see them — matches the UI's "hide out of stock" checkbox, checked
		// by default.
		ShowOutOfStock: q.Get("show_out_of_stock") == "1" || strings.EqualFold(q.Get("show_out_of_stock"), "true"),
	}
}

// fail answers a failed catalog query: 400 for a filter the caller got
// wrong (e.g. an invalid regexp), 500 otherwise.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var bad *catalog.InvalidFilterError
	if errors.As(err, &bad) {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.writeError(w, http.StatusInternalServerError, err)
}

func (s *Server) toResultJSON(row store.SetSearchRow) searchResultJSON {
	out := searchResultJSON{
		SetID:        row.SetID,
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
	}
	cur, low := s.catalog.Display(row.CurrentCents, row.Currency), s.catalog.Display(row.LowestCents, row.Currency)
	out.CurrentCents, out.Currency = cur.Cents, cur.Currency
	out.LowestCents = low.Cents
	out.OriginalCents, out.OriginalCurrency = cur.OriginalCents, cur.OriginalCurrency
	return out
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	rows, err := s.catalog.Search(r.Context(), filter(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]searchResultJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toResultJSON(row))
	}
	s.writeJSON(w, out)
}

// productJSON is one kit with every shop's listing of it.
type productJSON struct {
	ProductID int64                `json:"product_id"`
	Name      string               `json:"name"`
	Grade     string               `json:"grade"`
	Listings  []productListingJSON `json:"listings"`
}

type productListingJSON struct {
	searchResultJSON
	// MatchMethod is how this listing was grouped (ean, name, manual);
	// empty for a kit seen at a single shop.
	MatchMethod string `json:"match_method"`
	// Cheapest marks the lowest-priced buyable listing of a kit sold by
	// two or more shops (all of them, on a tie).
	Cheapest bool `json:"cheapest"`
}

// handleProducts is /api/search with listings of the same kit grouped
// together.
func (s *Server) handleProducts(w http.ResponseWriter, r *http.Request) {
	products, err := s.catalog.Products(r.Context(), filter(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]productJSON, 0, len(products))
	for _, p := range products {
		pj := productJSON{ProductID: p.ID, Name: p.Name, Grade: p.Grade, Listings: make([]productListingJSON, 0, len(p.Listings))}
		for _, l := range p.Listings {
			pj.Listings = append(pj.Listings, productListingJSON{
				searchResultJSON: s.toResultJSON(l.SetSearchRow),
				MatchMethod:      l.MatchMethod,
				Cheapest:         l.Cheapest,
			})
		}
		out = append(out, pj)
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
