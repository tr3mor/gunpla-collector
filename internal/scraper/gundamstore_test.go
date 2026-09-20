package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGundamStore_FetchAll(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/collections/mg-master-grade/products.json", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Write([]byte(`{"products": [
				{"id": 101, "title": "MG One", "handle": "mg-one", "variants": [{"available": true, "price": "92.00"}]},
				{"id": 102, "title": "MG Two", "handle": "mg-two", "variants": [{"available": false, "price": "54.99"}]}
			]}`))
		default:
			w.Write([]byte(`{"products": []}`))
		}
	})
	mux.HandleFunc("/collections/hg-high-grade/products.json", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Write([]byte(`{"products": [
				{"id": 201, "title": "HG One", "handle": "hg-one", "variants": [{"available": true, "price": "19.99"}]}
			]}`))
		default:
			w.Write([]byte(`{"products": []}`))
		}
	})
	mux.HandleFunc("/collections/rg-real-grade/products.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"products": []}`))
	})
	mux.HandleFunc("/collections/perfect-grade/products.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"products": []}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := &GundamStore{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/collections/mg-master-grade",
	}

	sets, err := g.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(sets) != 3 {
		t.Fatalf("got %d sets, want 3", len(sets))
	}

	byID := map[string]ScrapedSet{}
	for _, s := range sets {
		byID[s.ExternalID] = s
	}

	one, ok := byID["101"]
	if !ok {
		t.Fatal("missing set 101")
	}
	if one.Name != "MG One" || one.Grade != "MG" || one.PriceCents != 9200 || one.Currency != "USD" {
		t.Errorf("set 101 = %+v, unexpected fields", one)
	}
	if one.InStock == nil || !*one.InStock {
		t.Errorf("set 101 expected in stock")
	}
	if one.URL != srv.URL+"/products/mg-one" {
		t.Errorf("set 101 URL = %q, want %q", one.URL, srv.URL+"/products/mg-one")
	}

	two := byID["102"]
	if two.InStock == nil || *two.InStock {
		t.Errorf("set 102 expected out of stock")
	}
	if two.PriceCents != 5499 {
		t.Errorf("set 102 price = %d, want 5499", two.PriceCents)
	}

	hgOne := byID["201"]
	if hgOne.Grade != "HG" {
		t.Errorf("set 201 grade = %q, want HG", hgOne.Grade)
	}
}

func TestGundamStore_FetchAll_PaginatesUntilShortPage(t *testing.T) {
	mux := http.NewServeMux()

	// Full page (2 items, well under the real 250 cap but enough to prove
	// the loop advances) followed by a short page that ends pagination.
	mux.HandleFunc("/collections/mg-master-grade/products.json", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		limit := r.URL.Query().Get("limit")
		if limit != fmt.Sprint(gundamStorePageLimit) {
			t.Errorf("expected limit=%d, got %q", gundamStorePageLimit, limit)
		}
		switch page {
		case "1":
			products := make([]string, gundamStorePageLimit)
			for i := range products {
				products[i] = fmt.Sprintf(`{"id": %d, "title": "Kit %d", "handle": "kit-%d", "variants": [{"available": true, "price": "10.00"}]}`, i, i, i)
			}
			fmt.Fprintf(w, `{"products": [%s]}`, strings.Join(products, ","))
		case "2":
			w.Write([]byte(`{"products": [{"id": 9999, "title": "Last Kit", "handle": "last-kit", "variants": [{"available": true, "price": "10.00"}]}]}`))
		default:
			t.Errorf("unexpected page %q requested — should have stopped after the short page", page)
			w.Write([]byte(`{"products": []}`))
		}
	})
	for _, handle := range []string{"hg-high-grade", "rg-real-grade", "perfect-grade"} {
		mux.HandleFunc("/collections/"+handle+"/products.json", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"products": []}`))
		})
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := &GundamStore{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/collections/mg-master-grade",
	}

	sets, err := g.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(sets) != gundamStorePageLimit+1 {
		t.Fatalf("got %d sets, want %d", len(sets), gundamStorePageLimit+1)
	}
}

func TestGundamStore_FetchAll_NoProductsIsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/collections/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"products": []}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := &GundamStore{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/collections/mg-master-grade",
	}

	if _, err := g.FetchAll(context.Background()); err == nil {
		t.Fatal("expected error when no products found in any collection, got nil")
	}
}
