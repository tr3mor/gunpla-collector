package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gunpla-collector/internal/catalog"
	"gunpla-collector/internal/fx"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

func (f *fakeCatalog) History(ctx context.Context, productID, setID int64) ([]catalog.Series, error) {
	f.histArgs = [2]int64{productID, setID}
	return f.series, f.err
}

func TestHandleHistory_RequestErrors(t *testing.T) {
	cases := []struct {
		name   string
		target string
		err    error
		want   int
	}{
		{"no identifier", "/api/history", nil, http.StatusBadRequest},
		{"both identifiers", "/api/history?product=1&set=2", nil, http.StatusBadRequest},
		{"not a number", "/api/history?product=abc", nil, http.StatusBadRequest},
		{"zero", "/api/history?set=0", nil, http.StatusBadRequest},
		{"negative", "/api/history?set=-3", nil, http.StatusBadRequest},
		{"unknown kit", "/api/history?product=99", store.ErrKitNotFound, http.StatusNotFound},
		{"other failure", "/api/history?product=1", context.DeadlineExceeded, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fc := newTestServer()
			fc.err = tc.err
			if rec := get(t, s, tc.target); rec.Code != tc.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestHandleHistory_PassesTheKitToTheCatalog(t *testing.T) {
	s, fc := newTestServer()
	get(t, s, "/api/history?product=7")
	if fc.histArgs != [2]int64{7, 0} {
		t.Errorf("product query asked for %v, want product 7", fc.histArgs)
	}
	get(t, s, "/api/history?set=5")
	if fc.histArgs != [2]int64{0, 5} {
		t.Errorf("set query asked for %v, want set 5", fc.histArgs)
	}
}

// TestHandleHistory_EndToEnd drives the real catalog and store over a
// temporary database: a USD shop, a shop that drops the kit, and a token
// that reads must not need.
func TestHandleHistory_EndToEnd(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()

	collect := func(shopID int64, at string, rates fx.Rates, sets ...scraper.ScrapedSet) {
		runID, err := st.StartRun(ctx, shopID, at)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ApplyRun(ctx, shopID, runID, sets, at, rates); err != nil {
			t.Fatal(err)
		}
	}
	eu, _ := st.GetOrCreateShop(ctx, "eu", "EU Shop", "https://eu")
	us, _ := st.GetOrCreateShop(ctx, "us", "US Shop", "https://us")
	kitEU := func(price int, av scraper.Availability) scraper.ScrapedSet {
		return scraper.ScrapedSet{ExternalID: "1", URL: "https://eu/1", Name: "MG RX-78-2 Gundam Ver. Ka 1/100", Grade: "MG", PriceCents: price, Currency: "EUR", Availability: av}
	}
	kitUS := scraper.ScrapedSet{ExternalID: "9", URL: "https://us/9", Name: "MG – RX-78-2 Gundam Ver.Ka", Grade: "MG", PriceCents: 4200, Currency: "USD", Availability: scraper.AvailabilityInStock}
	other := scraper.ScrapedSet{ExternalID: "2", URL: "https://eu/2", Name: "HG Other", PriceCents: 1000, Currency: "EUR"}

	collect(eu.ID, "2026-01-01T18:00:00Z", nil, kitEU(4000, scraper.AvailabilityInStock))
	collect(us.ID, "2026-01-01T18:05:00Z", fx.Rates{"USD": 0.9}, kitUS)
	// collect regroups after every run; both listings are active here.
	cat := catalog.New(st)
	if _, err := cat.Regroup(ctx); err != nil {
		t.Fatal(err)
	}
	collect(eu.ID, "2026-01-02T18:00:00Z", nil, kitEU(4000, scraper.AvailabilityOutOfStock))
	collect(eu.ID, "2026-01-03T18:00:00Z", nil, other) // EU Shop drops the kit
	if _, err := cat.Regroup(ctx); err != nil {
		t.Fatal(err)
	}
	listings, _ := st.Listings(ctx)
	var pid int64
	for _, l := range listings {
		if l.Name == "MG RX-78-2 Gundam Ver. Ka 1/100" {
			pid = l.ProductID
		}
	}
	if pid == 0 {
		t.Fatal("the two MG Kit listings were not grouped")
	}

	srv := NewServer(cat, &fakeCatalog{}, fakeShops{}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithWriteToken("s3cret"))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history?product="+itoa(pid), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d without a token, want 200: %s", rec.Code, rec.Body)
	}
	var got []historySeriesJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ShopName != "EU Shop" || got[1].ShopName != "US Shop" {
		t.Fatalf("series = %+v, want EU Shop then US Shop", got)
	}

	euS, usS := got[0], got[1]
	if euS.Active || !usS.Active {
		t.Errorf("active: EU %v, US %v; want EU dropped the kit, US still lists it", euS.Active, usS.Active)
	}
	if len(euS.Entries) != 3 || !euS.Entries[0].Listed || euS.Entries[1].Availability != "out_of_stock" || euS.Entries[2].Listed {
		t.Errorf("EU entries = %+v, want buyable, out of stock, then not listed", euS.Entries)
	}
	if e := euS.Entries[0]; e.PriceCents != 4000 || e.OriginalCurrency != "" {
		t.Errorf("EU first entry = %+v, want 4000 EUR and no original price", e)
	}
	if len(usS.Entries) != 1 {
		t.Fatalf("US entries = %+v, want the one run", usS.Entries)
	}
	if e := usS.Entries[0]; e.PriceCents != 3780 || e.OriginalCents != 4200 || e.OriginalCurrency != "USD" {
		t.Errorf("US entry = %+v, want 3780 EUR beside 4200 USD", e)
	}

	// An unknown kit is a 404 from the real store too.
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history?set=9999", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown listing: status = %d, want 404", rec.Code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// A flat search result says which kit it belongs to, so the page can open
// the whole kit's history from the ungrouped list.
func TestHandleSearch_CarriesProductID(t *testing.T) {
	s, fc := newTestServer()
	fc.rows = []store.SetSearchRow{
		{SetID: 1, ProductID: 7, ShopSlug: "a", Name: "Grouped", Currency: "EUR"},
		{SetID: 2, ShopSlug: "a", Name: "Alone", Currency: "EUR"},
	}
	out := decodeSearch(t, get(t, s, "/api/search").Body.Bytes())
	if len(out) != 2 || out[0].ProductID != 7 || out[1].ProductID != 0 {
		t.Errorf("product ids = %d, %d; want 7 and 0", out[0].ProductID, out[1].ProductID)
	}
}
