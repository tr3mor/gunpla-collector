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
	"sort"
	"strings"

	"gunpla-collector/internal/fx"
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
	// eurRates maps a currency code to its EUR value; prices in those
	// currencies are shown converted. EUR (and unknown currencies) pass
	// through unchanged.
	eurRates fx.Rates
}

// Option configures a Server.
type Option func(*Server)

// WithEURRates sets the currency -> EUR conversion rates.
func WithEURRates(rates fx.Rates) Option {
	return func(s *Server) { s.eurRates = rates }
}

func NewServer(ds DataStore, logger *slog.Logger, opts ...Option) *Server {
	s := &Server{store: ds, logger: logger}
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
	// OriginalCents/OriginalCurrency hold the shop's own price when it was
	// converted to EUR (Currency is then "EUR"); zero/empty otherwise.
	OriginalCents    int    `json:"original_price_cents,omitempty"`
	OriginalCurrency string `json:"original_currency,omitempty"`
}

// filterRows applies the request's search filters (q, regexp, shop, grade,
// show_out_of_stock) to the active listings. A non-nil error is a bad
// request (e.g. an invalid regexp) when badRequest is true.
func (s *Server) filterRows(r *http.Request) (rows []store.SetSearchRow, badRequest bool, err error) {
	q := r.URL.Query()
	nameSubstr := strings.TrimSpace(q.Get("q"))
	nameRegexp := strings.TrimSpace(q.Get("regexp"))
	shopSlug := strings.TrimSpace(q.Get("shop"))
	// "grade" is MG/HG/RG/PG (case-insensitive), or "none" for sets with no
	// grade (e.g. most pre-orders).
	grade := strings.TrimSpace(q.Get("grade"))
	// Out-of-stock sets are hidden unless the caller explicitly asks to see
	// them — matches the UI's "hide out of stock" checkbox, checked by
	// default.
	showOutOfStock := q.Get("show_out_of_stock") == "1" || strings.EqualFold(q.Get("show_out_of_stock"), "true")

	var re *regexp.Regexp
	if nameRegexp != "" {
		// Case-insensitive by default — (?i) is harmless to prepend even if
		// the caller already wrote their own flags.
		re, err = regexp.Compile("(?i)" + nameRegexp)
		if err != nil {
			return nil, true, err
		}
	}

	all, err := s.store.SearchSets(r.Context(), shopSlug)
	if err != nil {
		return nil, false, err
	}
	for _, row := range all {
		if nameSubstr != "" && !strings.Contains(strings.ToLower(row.Name), strings.ToLower(nameSubstr)) {
			continue
		}
		if grade != "" {
			if strings.EqualFold(grade, "none") {
				if row.Grade != "" {
					continue
				}
			} else if !strings.EqualFold(row.Grade, grade) {
				continue
			}
		}
		if re != nil && !re.MatchString(row.Name) {
			continue
		}
		if !showOutOfStock && row.Availability == scraper.AvailabilityOutOfStock {
			continue
		}
		rows = append(rows, row)
	}
	return rows, false, nil
}

func (s *Server) failFilter(w http.ResponseWriter, badRequest bool, err error) {
	if badRequest {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.writeError(w, http.StatusInternalServerError, err)
}

func (s *Server) toResultJSON(row store.SetSearchRow) searchResultJSON {
	out := searchResultJSON{
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
	if cents, ok := s.eurRates.ToEUR(row.CurrentCents, row.Currency); ok {
		out.OriginalCents, out.OriginalCurrency = row.CurrentCents, row.Currency
		out.CurrentCents = cents
		out.LowestCents, _ = s.eurRates.ToEUR(row.LowestCents, row.Currency)
		out.Currency = "EUR"
	}
	return out
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	rows, bad, err := s.filterRows(r)
	if err != nil {
		s.failFilter(w, bad, err)
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
	SetID int64 `json:"set_id"`
	// MatchMethod is how this listing was grouped (ean, name, manual);
	// empty for a kit seen at a single shop.
	MatchMethod string `json:"match_method"`
	// Cheapest marks the lowest-priced buyable listing of a kit sold by
	// two or more shops (all of them, on a tie).
	Cheapest bool `json:"cheapest"`
}

// handleProducts is /api/search with listings of the same kit grouped
// together. A kit is returned if any of its listings passes the filters,
// and with every listing that does, so the cross-shop price comparison
// stays visible. Kits sold by more shops come first.
func (s *Server) handleProducts(w http.ResponseWriter, r *http.Request) {
	rows, bad, err := s.filterRows(r)
	if err != nil {
		s.failFilter(w, bad, err)
		return
	}

	byProduct := map[int64]*productJSON{}
	var order []int64
	for _, row := range rows {
		// A listing the matcher hasn't seen yet stands alone, keyed by a
		// negative id that can't collide with a product id.
		key := row.ProductID
		if key == 0 {
			key = -row.SetID
		}
		p, ok := byProduct[key]
		if !ok {
			name := row.ProductName
			if name == "" {
				name = row.Name
			}
			p = &productJSON{ProductID: row.ProductID, Name: name, Grade: row.Grade}
			byProduct[key] = p
			order = append(order, key)
		}
		p.Listings = append(p.Listings, productListingJSON{
			searchResultJSON: s.toResultJSON(row),
			SetID:            row.SetID,
			MatchMethod:      row.MatchMethod,
		})
	}

	out := make([]*productJSON, 0, len(order))
	for _, key := range order {
		p := byProduct[key]
		markCheapest(p.Listings)
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Listings) != len(out[j].Listings) {
			return len(out[i].Listings) > len(out[j].Listings)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	s.writeJSON(w, out)
}

// markCheapest flags the cheapest in-stock (or pre-order) EUR listing, but
// only when at least two shops list the kit — a lone listing is trivially
// "cheapest". Out-of-stock listings never win; listings still in another
// currency (no configured rate) aren't comparable and are skipped.
func markCheapest(ls []productListingJSON) {
	shops := map[string]bool{}
	for _, l := range ls {
		shops[l.ShopSlug] = true
	}
	if len(shops) < 2 {
		return
	}
	best := -1
	for _, l := range ls {
		if l.Currency != "EUR" || l.Availability == string(scraper.AvailabilityOutOfStock) {
			continue
		}
		if best == -1 || l.CurrentCents < best {
			best = l.CurrentCents
		}
	}
	for i, l := range ls {
		if best != -1 && l.Currency == "EUR" && l.Availability != string(scraper.AvailabilityOutOfStock) && l.CurrentCents == best {
			ls[i].Cheapest = true
		}
	}
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
