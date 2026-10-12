package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"gunpla-collector/internal/catalog"
	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/store"
)

// fakeCatalog returns canned results and records the filter it was asked for.
type fakeCatalog struct {
	rows     []store.SetSearchRow
	products []catalog.Product
	err      error
	lastSeen catalog.Filter
	statuses []catalog.WatchStatus
	saved    []catalog.Watch
	review   []catalog.ReviewRow
	linked   [][2]int64
	rates    fx.Rates
}

func (f *fakeCatalog) Display(cents int, currency string) fx.Price {
	return f.rates.Display(cents, currency)
}

func (f *fakeCatalog) Search(ctx context.Context, flt catalog.Filter) ([]store.SetSearchRow, error) {
	f.lastSeen = flt
	return f.rows, f.err
}

func (f *fakeCatalog) Products(ctx context.Context, flt catalog.Filter) ([]catalog.Product, error) {
	f.lastSeen = flt
	return f.products, f.err
}

func (f *fakeCatalog) SpecFromSet(ctx context.Context, setID int64) (catalog.WatchSpec, string, error) {
	return catalog.WatchSpec{}, "", f.err
}

func (f *fakeCatalog) PreviewWatch(ctx context.Context, spec catalog.WatchSpec) (catalog.WatchPreview, error) {
	return catalog.WatchPreview{Spec: spec}, f.err
}

func (f *fakeCatalog) WatchStatuses(ctx context.Context) ([]catalog.WatchStatus, error) {
	return f.statuses, f.err
}

func (f *fakeCatalog) SaveWatch(ctx context.Context, w catalog.Watch) (catalog.Watch, error) {
	f.saved = append(f.saved, w)
	if w.ID == 0 {
		w.ID = 7
	}
	return w, f.err
}

func (f *fakeCatalog) DeleteWatch(ctx context.Context, id int64) error { return f.err }

func (f *fakeCatalog) NearMisses(ctx context.Context, includeDismissed bool) ([]catalog.ReviewRow, error) {
	return f.review, f.err
}

func (f *fakeCatalog) Link(ctx context.Context, a, b int64) error {
	f.linked = append(f.linked, [2]int64{a, b})
	return f.err
}

func (f *fakeCatalog) DismissMatch(ctx context.Context, a, b int64) error { return f.err }
func (f *fakeCatalog) RestoreMatch(ctx context.Context, a, b int64) error { return f.err }

type fakeShops []store.Shop

func (f fakeShops) ActiveShops(ctx context.Context) ([]store.Shop, error) { return f, nil }

func newTestServer() (*Server, *fakeCatalog) {
	fc := &fakeCatalog{
		rows: []store.SetSearchRow{
			{ShopSlug: "shop-a", ShopName: "Shop A", Name: "RX-78-2 Gundam", Grade: "MG", URL: "https://a/rx78", CurrentCents: 4000, Currency: "EUR", LowestCents: 3500, ScrapedAt: "2026-01-03T00:00:00Z"},
			{ShopSlug: "shop-b", ShopName: "Shop B", Name: "RX-93 Nu Gundam", Grade: "RG", URL: "https://b/rx93", CurrentCents: 3000, Currency: "USD", LowestCents: 3000, ScrapedAt: "2026-01-03T00:00:00Z"},
		},
	}
	shops := fakeShops{
		{ID: 1, Slug: "shop-a", Name: "Shop A"},
		{ID: 2, Slug: "shop-b", Name: "Shop B"},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(fc, shops, logger), fc
}

func get(t *testing.T, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func decodeSearch(t *testing.T, body []byte) []searchResultJSON {
	t.Helper()
	var out []searchResultJSON
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode search response: %v\nbody: %s", err, body)
	}
	return out
}

func TestHandleSearch_ReturnsCatalogRows(t *testing.T) {
	s, _ := newTestServer()
	rec := get(t, s, "/api/search")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	out := decodeSearch(t, rec.Body.Bytes())
	if len(out) != 2 || out[0].Name != "RX-78-2 Gundam" || out[0].ShopSlug != "shop-a" || out[0].LowestCents != 3500 {
		t.Fatalf("out = %+v", out)
	}
}

func TestFilterFromQuery(t *testing.T) {
	s, fc := newTestServer()
	for path, want := range map[string]catalog.Filter{
		"/api/search": {},
		"/api/search?q=%20gundam%20&regexp=%5ERX&shop=shop-b&grade=hg": {Query: "gundam", Regexp: "^RX", Shop: "shop-b", Grade: "hg"},
		"/api/search?show_out_of_stock=1":                              {ShowOutOfStock: true},
		"/api/products?show_out_of_stock=TRUE&q=zaku":                  {ShowOutOfStock: true, Query: "zaku"},
		"/api/products?show_out_of_stock=0":                            {},
	} {
		get(t, s, path)
		if fc.lastSeen != want {
			t.Errorf("%s: filter = %+v, want %+v", path, fc.lastSeen, want)
		}
	}
}

func TestInvalidFilterIs400OthersAre500(t *testing.T) {
	s, fc := newTestServer()
	fc.err = &catalog.InvalidFilterError{Err: errors.New("bad regexp")}
	for _, path := range []string{"/api/search", "/api/products"} {
		rec := get(t, s, path)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body: %s", path, rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != "bad regexp" {
			t.Errorf("%s: body = %s", path, rec.Body.String())
		}
	}
	fc.err = errors.New("db down")
	for _, path := range []string{"/api/search", "/api/products"} {
		if rec := get(t, s, path); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", path, rec.Code)
		}
	}
}

func TestHandleShops(t *testing.T) {
	s, _ := newTestServer()
	rec := get(t, s, "/api/shops")
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
	rec := get(t, s, "/")
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

func TestHandleProducts_EncodesProductsAndConvertsToEUR(t *testing.T) {
	s, fc := newTestServer()
	fc.rates = fx.Rates{"USD": 0.9}
	fc.products = []catalog.Product{
		{ID: 7, Name: "Kit", Grade: "MG", Listings: []catalog.Listing{
			{SetSearchRow: store.SetSearchRow{SetID: 1, ProductID: 7, ShopSlug: "us", ShopName: "US", Name: "Kit", URL: "u1", CurrentCents: 10000, LowestCents: 9000, Currency: "USD", MatchMethod: "ean"}, Cheapest: true},
			{SetSearchRow: store.SetSearchRow{SetID: 2, ProductID: 7, ShopSlug: "nl", ShopName: "NL", Name: "Kit", URL: "u2", CurrentCents: 9500, LowestCents: 9500, Currency: "EUR"}},
		}},
		{Name: "Solo", Listings: []catalog.Listing{
			{SetSearchRow: store.SetSearchRow{SetID: 3, ShopSlug: "us", Name: "Solo", URL: "u3", CurrentCents: 1000, LowestCents: 1000, Currency: "USD"}},
		}},
	}
	rec := get(t, s, "/api/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var got []productJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ProductID != 7 || got[0].Name != "Kit" || got[0].Grade != "MG" || len(got[0].Listings) != 2 {
		t.Fatalf("products = %+v", got)
	}
	us := got[0].Listings[0]
	if us.SetID != 1 || us.MatchMethod != "ean" || !us.Cheapest ||
		us.Currency != "EUR" || us.CurrentCents != 9000 || us.LowestCents != 8100 || us.OriginalCents != 10000 || us.OriginalCurrency != "USD" {
		t.Errorf("converted listing = %+v", us)
	}
	if nl := got[0].Listings[1]; nl.Cheapest || nl.CurrentCents != 9500 || nl.OriginalCurrency != "" {
		t.Errorf("EUR listing = %+v", nl)
	}
	if solo := got[1]; solo.ProductID != 0 || solo.Listings[0].CurrentCents != 900 {
		t.Errorf("single-listing product = %+v", solo)
	}
}

func TestHandleProducts_EmptyIsAnEmptyArray(t *testing.T) {
	s, _ := newTestServer()
	rec := get(t, s, "/api/products")
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("body = %q, want []", body)
	}
}

func TestHandleSearch_ConvertsToEUR(t *testing.T) {
	s, fc := newTestServer()
	fc.rates = fx.Rates{"USD": 0.5}
	got := decodeSearch(t, get(t, s, "/api/search").Body.Bytes())
	if len(got) != 2 || got[1].Currency != "EUR" || got[1].CurrentCents != 1500 {
		t.Errorf("got %+v, want the $30.00 Nu Gundam as EUR 15.00", got)
	}
}

func TestHandleSearch_NoRateKeepsShopCurrency(t *testing.T) {
	s, _ := newTestServer()
	got := decodeSearch(t, get(t, s, "/api/search").Body.Bytes())
	if us := got[1]; us.Currency != "USD" || us.CurrentCents != 3000 || us.LowestCents != 3000 || us.OriginalCurrency != "" {
		t.Errorf("got %+v, want the USD price untouched", us)
	}
}
