package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"gunpla-collector/internal/catalog"
	"gunpla-collector/internal/matcher"
	"gunpla-collector/internal/store"
	"gunpla-collector/internal/watchlist"
)

func send(s *Server, method, target, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

const watchBody = `{"label":"Ka","spec":{"grade":"MG","model":"rx782","tokens":[],"variants":["verka"]},"max_price_cents":5000,"on_restock":true,"enabled":true}`

func TestWatchSave_CreateUpdateDelete(t *testing.T) {
	s, fc := newTestServer()
	rec := send(s, "POST", "/api/watches", watchBody, nil)
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var got watchJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID != 7 || got.Spec.Model != "rx782" || got.MaxPriceCents != 5000 {
		t.Fatalf("created = %+v err %v", got, err)
	}
	if rec = send(s, "PUT", "/api/watches/12", watchBody, nil); rec.Code != 200 || fc.saved[1].ID != 12 {
		t.Fatalf("update: %d, saved %+v", rec.Code, fc.saved)
	}
	// A client-supplied id on create is ignored: POST always creates.
	send(s, "POST", "/api/watches", strings.Replace(watchBody, `{"label"`, `{"id":99,"label"`, 1), nil)
	if fc.saved[2].ID != 0 {
		t.Errorf("POST kept client id %d", fc.saved[2].ID)
	}
	if rec = send(s, "PUT", "/api/watches/abc", watchBody, nil); rec.Code != 400 {
		t.Errorf("bad id: %d", rec.Code)
	}
	if rec = send(s, "POST", "/api/watches", `{not json`, nil); rec.Code != 400 {
		t.Errorf("bad json: %d", rec.Code)
	}
	if rec = send(s, "DELETE", "/api/watches/7", "", nil); rec.Code != 200 {
		t.Errorf("delete: %d", rec.Code)
	}
}

func TestWatchSave_MapsErrors(t *testing.T) {
	s, fc := newTestServer()
	fc.err = &watchlist.InvalidError{Reason: "a watch needs a name"}
	if rec := send(s, "POST", "/api/watches", watchBody, nil); rec.Code != 400 || !strings.Contains(rec.Body.String(), "needs a name") {
		t.Errorf("invalid: %d %s", rec.Code, rec.Body)
	}
	fc.err = store.ErrWatchNotFound
	if rec := send(s, "DELETE", "/api/watches/3", "", nil); rec.Code != 404 {
		t.Errorf("missing: %d", rec.Code)
	}
	fc.err = errors.New("db down")
	if rec := send(s, "POST", "/api/watches", watchBody, nil); rec.Code != 500 {
		t.Errorf("internal: %d", rec.Code)
	}
}

func TestWrites_AreGuarded(t *testing.T) {
	s, fc := newTestServer()
	// A page on another site can't drive the user's browser.
	for name, hdr := range map[string]map[string]string{
		"foreign origin": {"Origin": "https://evil.example"},
		"cross-site":     {"Sec-Fetch-Site": "cross-site"},
	} {
		if rec := send(s, "POST", "/api/watches", watchBody, hdr); rec.Code != 403 {
			t.Errorf("%s: %d, want 403", name, rec.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/watches", strings.NewReader(watchBody))
	req.Header.Set("Content-Type", "text/plain") // what a cross-site <form> can send
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("text/plain body: %d, want 403", rec.Code)
	}
	if len(fc.saved) != 0 {
		t.Fatalf("a refused request still saved: %+v", fc.saved)
	}
	// Same-origin pages and plain clients are fine.
	if rec := send(s, "POST", "/api/watches", watchBody, map[string]string{"Origin": "http://example.com"}); rec.Code != 200 {
		t.Errorf("same origin: %d %s", rec.Code, rec.Body)
	}
}

func TestWrites_RequireTheTokenWhenSet(t *testing.T) {
	fc := &fakeCatalog{}
	s := NewServer(fc, fc, fakeShops{}, slog.New(slog.NewTextHandler(io.Discard, nil)), WithWriteToken("s3cret"))
	if rec := send(s, "POST", "/api/watches", watchBody, nil); rec.Code != 401 || rec.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := send(s, "POST", "/api/watches", watchBody, map[string]string{"Authorization": "Bearer wrong"}); rec.Code != 401 {
		t.Errorf("wrong token: %d", rec.Code)
	}
	if rec := send(s, "POST", "/api/watches", watchBody, map[string]string{"Authorization": "s3cret"}); rec.Code != 401 {
		t.Errorf("token without the Bearer prefix: %d, want 401", rec.Code)
	}
	if rec := send(s, "POST", "/api/watches", watchBody, map[string]string{"Authorization": "Bearer s3cret"}); rec.Code != 200 {
		t.Errorf("right token: %d %s", rec.Code, rec.Body)
	}
	// Reading never needs it, and the UI can find out whether it must ask.
	if rec := send(s, "GET", "/api/watches", "", nil); rec.Code != 200 {
		t.Errorf("read: %d", rec.Code)
	}
	if rec := send(s, "GET", "/api/config", "", nil); !strings.Contains(rec.Body.String(), `"write_token_required":true`) {
		t.Errorf("config: %s", rec.Body)
	}
}

func TestHandleWatches_ListsStatusAndWatchedSets(t *testing.T) {
	s, fc := newTestServer()
	row := watchlist.Listing{SetID: 5, ShopName: "A", Name: "MG RX-78-2", CurrentCents: 4000, Currency: "EUR", HasEUR: true, EURCents: 4000}
	fc.statuses = []watchlist.Status{
		{Watch: watchlist.Watch{ID: 1, Label: "on", Enabled: true}, Hit: true, Preview: watchlist.Preview{
			Matches:  []watchlist.Match{{Listing: row, Best: true}, {Listing: watchlist.Listing{SetID: 6}, Excluded: true}},
			Variants: []watchlist.Variant{{Label: "Ver.Ka", Selected: true}, {Label: "Base release"}},
		}},
		{Watch: watchlist.Watch{ID: 2, Label: "paused"}, Preview: watchlist.Preview{Matches: []watchlist.Match{{Listing: watchlist.Listing{SetID: 9}}}}},
	}
	var out watchesJSON
	if err := json.Unmarshal(send(s, "GET", "/api/watches", "", nil).Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Watches) != 2 || !out.Watches[0].Hit || out.Watches[0].Listings != 1 || out.Watches[0].Best == nil || out.Watches[0].Best.SetID != 5 {
		t.Fatalf("watches = %+v", out.Watches)
	}
	if len(out.Watches[0].Versions) != 1 || out.Watches[0].Versions[0] != "Ver.Ka" || out.Watches[1].Best != nil {
		t.Errorf("versions/best wrong: %+v", out.Watches)
	}
	// Excluded listings and paused watches don't mark the search page.
	if len(out.WatchedSetIDs) != 1 || out.WatchedSetIDs[0] != 5 {
		t.Errorf("watched = %v, want [5]", out.WatchedSetIDs)
	}
	if !bytes.Contains(send(s, "GET", "/api/watches", "", nil).Body.Bytes(), []byte(`"missing_variants":[]`)) {
		t.Error("missing_variants must be [] not null")
	}
}

func TestReviewEndpoints(t *testing.T) {
	s, fc := newTestServer()
	mk := func(kind string, id1, id2 int64, shopA, shopB string) catalog.ReviewRow {
		return catalog.ReviewRow{
			ReviewItem: matcher.ReviewItem{A: matcher.Listing{ID: id1, Name: "RG Nu"}, B: matcher.Listing{ID: id2, Name: "RG Nu Ver"}, Kind: kind, Score: 0.9, Reason: "r"},
			RowA:       store.SetSearchRow{SetID: id1, ShopSlug: shopA, Name: "RG Nu"}, RowB: store.SetSearchRow{SetID: id2, ShopSlug: shopB, Name: "RG Nu Ver"},
		}
	}
	fc.review = []catalog.ReviewRow{mk("close", 1, 2, "a", "b"), mk("version", 3, 4, "a", "c"), mk("close", 5, 6, "b", "c")}

	var out reviewJSON
	if err := json.Unmarshal(send(s, "GET", "/api/review?kind=close&shop=a", "", nil).Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 1 || len(out.Items) != 1 || out.Items[0].A.SetID != 1 {
		t.Fatalf("filtered = %+v", out)
	}
	if out.Counts["close"] != 1 || out.Counts["version"] != 1 {
		t.Errorf("counts = %v, want per-kind counts under the shop filter", out.Counts)
	}
	out = reviewJSON{}
	json.Unmarshal(send(s, "GET", "/api/review?limit=1", "", nil).Body.Bytes(), &out)
	if out.Total != 3 || len(out.Items) != 1 {
		t.Errorf("limit: total %d items %d", out.Total, len(out.Items))
	}

	if rec := send(s, "POST", "/api/review/link", `{"a":1,"b":2}`, nil); rec.Code != 200 || len(fc.linked) != 1 || fc.linked[0] != [2]int64{1, 2} {
		t.Errorf("link: %d %v", rec.Code, fc.linked)
	}
	for _, body := range []string{`{"a":1,"b":1}`, `{"a":0,"b":2}`, `{}`} {
		if rec := send(s, "POST", "/api/review/dismiss", body, nil); rec.Code != 400 {
			t.Errorf("dismiss %s: %d, want 400", body, rec.Code)
		}
	}
	if rec := send(s, "POST", "/api/review/restore", `{"a":1,"b":2}`, map[string]string{"Origin": "https://evil.example"}); rec.Code != 403 {
		t.Errorf("review writes must be guarded too: %d", rec.Code)
	}
}
