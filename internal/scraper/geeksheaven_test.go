package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifyGrade(t *testing.T) {
	cases := []struct {
		title     string
		wantGrade string
		wantOK    bool
	}{
		{"Gundam MG Sets", "MG", true},
		{"Gundam HG Sets", "HG", true},
		{"Gundam RG Sets", "RG", true},
		{"Gundam Perfect Grade Sets", "PG", true},
		{"Gundam SD/NG Sets", "", false},
		{"Gundam 30MM Sets", "", false},
		{"Gundam TCG", "", false},
		{"Bandai Model Kits Compleet overzicht", "", false},
		{"Action Bases", "", false},
	}
	for _, c := range cases {
		grade, ok := classifyGrade(c.title)
		if ok != c.wantOK || grade != c.wantGrade {
			t.Errorf("classifyGrade(%q) = (%q, %v), want (%q, %v)", c.title, grade, ok, c.wantGrade, c.wantOK)
		}
	}
}

func TestGeeksHeaven_FetchAll(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/gundam-model-kits/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"catalog": {
				"categories": {
					"1": {"url": "gundam-model-kits/mg-cat", "title": "Gundam MG Sets"},
					"2": {"url": "gundam-model-kits/hg-cat", "title": "Gundam HG Sets"},
					"3": {"url": "gundam-model-kits/tcg-cat", "title": "Gundam TCG"}
				}
			}
		}`))
	})

	mux.HandleFunc("/gundam-model-kits/mg-cat/page1.ajax", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"page": 1, "pages": 2, "count": 3,
			"products": [
				{"id": 101, "sku": "s101", "ean": "e101", "available": true, "url": "mg-one.html", "title": "MG One", "price": {"price_incl": 54.99}},
				{"id": 102, "sku": "s102", "ean": "e102", "available": false, "url": "mg-two.html", "title": "MG Two", "price": {"price_incl": 129.99}}
			]
		}`))
	})
	mux.HandleFunc("/gundam-model-kits/mg-cat/page2.ajax", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"page": 2, "pages": 2, "count": 3,
			"products": [
				{"id": 103, "sku": "s103", "ean": "e103", "available": true, "url": "https://example.com/mg-three.html", "title": "MG Three", "price": {"price_incl": 42.5}}
			]
		}`))
	})
	mux.HandleFunc("/gundam-model-kits/hg-cat/page1.ajax", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"page": 1, "pages": 1, "count": 1,
			"products": [
				{"id": 201, "sku": "s201", "ean": "e201", "available": true, "url": "hg-one.html", "title": "HG One", "price": {"price_incl": 19.99}}
			]
		}`))
	})
	// If the scraper ever fetches the TCG category, fail the test.
	mux.HandleFunc("/gundam-model-kits/tcg-cat/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected fetch of non-grade category: %s", r.URL.Path)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := &GeeksHeaven{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/gundam-model-kits/",
	}

	sets, err := g.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(sets) != 4 {
		t.Fatalf("got %d sets, want 4", len(sets))
	}

	byID := map[string]ScrapedSet{}
	for _, s := range sets {
		byID[s.ExternalID] = s
	}

	one, ok := byID["101"]
	if !ok {
		t.Fatal("missing set 101")
	}
	if one.Name != "MG One" || one.Grade != "MG" || one.PriceCents != 5499 || one.Currency != "EUR" {
		t.Errorf("set 101 = %+v, unexpected fields", one)
	}
	if one.InStock == nil || !*one.InStock {
		t.Errorf("set 101 expected in stock")
	}
	if one.URL != srv.URL+"/mg-one.html" {
		t.Errorf("set 101 URL = %q, want relative URL resolved against host", one.URL)
	}

	two := byID["102"]
	if two.InStock == nil || *two.InStock {
		t.Errorf("set 102 expected out of stock")
	}

	three := byID["103"]
	if three.URL != "https://example.com/mg-three.html" {
		t.Errorf("set 103 URL = %q, want absolute URL preserved as-is", three.URL)
	}
	if three.PriceCents != 4250 {
		t.Errorf("set 103 price = %d, want 4250", three.PriceCents)
	}

	hgOne := byID["201"]
	if hgOne.Grade != "HG" {
		t.Errorf("set 201 grade = %q, want HG", hgOne.Grade)
	}
}
