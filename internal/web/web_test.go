package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"gunpla-collector/internal/store"
)

// fakeStore is an in-memory DataStore for testing handlers without SQLite.
type fakeStore struct {
	rows  []store.SetSearchRow
	shops []store.Shop
}

func (f *fakeStore) SearchSets(ctx context.Context, shopSlug string) ([]store.SetSearchRow, error) {
	if shopSlug == "" {
		return f.rows, nil
	}
	var out []store.SetSearchRow
	for _, r := range f.rows {
		if r.ShopSlug == shopSlug {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) ActiveShops(ctx context.Context) ([]store.Shop, error) {
	return f.shops, nil
}

func boolPtr(b bool) *bool { return &b }

func newTestServer() (*Server, *fakeStore) {
	fs := &fakeStore{
		rows: []store.SetSearchRow{
			{ShopSlug: "shop-a", ShopName: "Shop A", Name: "RX-78-2 Gundam", Grade: "MG", URL: "https://a/rx78", CurrentCents: 4000, Currency: "EUR", LowestCents: 3500, ScrapedAt: "2026-01-03T00:00:00Z"},
			{ShopSlug: "shop-a", ShopName: "Shop A", Name: "Zaku II", Grade: "HG", URL: "https://a/zaku", CurrentCents: 2000, Currency: "EUR", LowestCents: 2000, ScrapedAt: "2026-01-03T00:00:00Z"},
			{ShopSlug: "shop-b", ShopName: "Shop B", Name: "RX-93 Nu Gundam", Grade: "RG", URL: "https://b/rx93", CurrentCents: 3000, Currency: "USD", LowestCents: 3000, ScrapedAt: "2026-01-03T00:00:00Z"},
			{ShopSlug: "shop-b", ShopName: "Shop B", Name: "Sold Out Kit", Grade: "HG", URL: "https://b/sold-out", CurrentCents: 1000, Currency: "USD", CurrentInStock: boolPtr(false), LowestCents: 1000, ScrapedAt: "2026-01-03T00:00:00Z"},
		},
		shops: []store.Shop{
			{ID: 1, Slug: "shop-a", Name: "Shop A"},
			{ID: 2, Slug: "shop-b", Name: "Shop B"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(fs, logger), fs
}

func decodeSearch(t *testing.T, body []byte) []searchResultJSON {
	t.Helper()
	var out []searchResultJSON
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode search response: %v\nbody: %s", err, body)
	}
	return out
}

func TestHandleSearch_NoFilter_HidesOutOfStockByDefault(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (Sold Out Kit hidden by default)", len(out))
	}
	for _, r := range out {
		if r.Name == "Sold Out Kit" {
			t.Fatalf("out = %+v, Sold Out Kit should be hidden by default", out)
		}
	}
}

func TestHandleSearch_ShowOutOfStock(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?show_out_of_stock=1", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 4 {
		t.Fatalf("show_out_of_stock=1: len(out) = %d, want 4 (all rows including Sold Out Kit)", len(out))
	}
}

// A set with unknown stock status (CurrentInStock == nil, scraper doesn't
// report it) must never be treated as out of stock.
func TestHandleSearch_UnknownStockIsNotFilteredAsOutOfStock(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=zaku", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 1 || out[0].Name != "Zaku II" {
		t.Fatalf("q=zaku: out = %+v, want exactly [Zaku II] (unknown stock status must not be hidden)", out)
	}
}

func TestHandleSearch_SubstringFilter(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=gundam", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 2 {
		t.Fatalf("q=gundam: len(out) = %d, want 2 (case-insensitive match on both Gundams)", len(out))
	}
}

func TestHandleSearch_RegexpFilter(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?regexp=^RX-78", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 1 || out[0].Name != "RX-78-2 Gundam" {
		t.Fatalf("regexp=^RX-78: out = %+v, want exactly [RX-78-2 Gundam]", out)
	}
}

func TestHandleSearch_RegexpIsCaseInsensitive(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?regexp=zaku", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 1 || out[0].Name != "Zaku II" {
		t.Fatalf("regexp=zaku (lowercase): out = %+v, want exactly [Zaku II] (case-insensitive match)", out)
	}
}

func TestHandleSearch_InvalidRegexp_Returns400(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?regexp=(unclosed", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSearch_ShopFilter(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?shop=shop-b", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 1 || out[0].ShopSlug != "shop-b" {
		t.Fatalf("shop=shop-b: out = %+v, want exactly one shop-b row", out)
	}
}

func TestHandleSearch_CombinedSubstringAndRegexpAreANDed(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	// "gundam" substring matches both RX-78-2 and RX-93; the regexp narrows
	// to just the one starting with "RX-78".
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=gundam&regexp=^RX-78", nil))
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 1 || out[0].Name != "RX-78-2 Gundam" {
		t.Fatalf("q=gundam&regexp=^RX-78: out = %+v, want exactly [RX-78-2 Gundam]", out)
	}
}

func TestHandleShops(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shops", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
}

func TestHandleIndex_ServesHTML(t *testing.T) {
	s, _ := newTestServer()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("body is empty")
	}
}
