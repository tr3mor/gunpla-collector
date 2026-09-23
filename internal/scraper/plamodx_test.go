package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlamoDX_FetchAll(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/wp-json/wc/store/v1/products", func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("category")
		page := r.URL.Query().Get("page")
		switch {
		case cat == "master-grade-mg" && page == "1":
			w.Write([]byte(`[
				{"id": 101, "name": "MG One", "permalink": "https://plamodx.nl/product/mg-one/", "sku": "sku-101", "is_in_stock": true, "prices": {"price": "9200", "currency_minor_unit": 2}},
				{"id": 102, "name": "MG Two", "permalink": "https://plamodx.nl/product/mg-two/", "sku": "", "is_in_stock": false, "prices": {"price": "5499", "currency_minor_unit": 2}}
			]`))
		case cat == "high-grade-hg" && page == "1":
			w.Write([]byte(`[
				{"id": 201, "name": "HG &#8211; One", "permalink": "https://plamodx.nl/product/hg-one/", "sku": "", "is_in_stock": true, "prices": {"price": "1999", "currency_minor_unit": 2}}
			]`))
		default:
			w.Write([]byte(`[]`))
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &PlamoDX{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/product-category/gunpla/",
	}

	sets, err := p.FetchAll(context.Background())
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
	if one.Name != "MG One" || one.Grade != "MG" || one.PriceCents != 9200 || one.Currency != "EUR" {
		t.Errorf("set 101 = %+v, unexpected fields", one)
	}
	if one.InStock == nil || !*one.InStock {
		t.Errorf("set 101 expected in stock")
	}
	if one.URL != "https://plamodx.nl/product/mg-one/" {
		t.Errorf("set 101 URL = %q, unexpected", one.URL)
	}
	if one.SKU != "sku-101" {
		t.Errorf("set 101 SKU = %q, want sku-101", one.SKU)
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
	if hgOne.Name != "HG – One" {
		t.Errorf("set 201 name = %q, want HTML entities decoded", hgOne.Name)
	}
}

func TestPlamoDX_FetchAll_PaginatesUntilShortPage(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/wp-json/wc/store/v1/products", func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("category")
		if cat != "master-grade-mg" {
			w.Write([]byte(`[]`))
			return
		}
		page := r.URL.Query().Get("page")
		perPage := r.URL.Query().Get("per_page")
		if perPage != fmt.Sprint(plamodxPageLimit) {
			t.Errorf("expected per_page=%d, got %q", plamodxPageLimit, perPage)
		}
		switch page {
		case "1":
			products := make([]string, plamodxPageLimit)
			for i := range products {
				products[i] = fmt.Sprintf(`{"id": %d, "name": "Kit %d", "permalink": "https://plamodx.nl/product/kit-%d/", "is_in_stock": true, "prices": {"price": "1000", "currency_minor_unit": 2}}`, i, i, i)
			}
			fmt.Fprintf(w, `[%s]`, strings.Join(products, ","))
		case "2":
			w.Write([]byte(`[{"id": 9999, "name": "Last Kit", "permalink": "https://plamodx.nl/product/last-kit/", "is_in_stock": true, "prices": {"price": "1000", "currency_minor_unit": 2}}]`))
		default:
			t.Errorf("unexpected page %q requested — should have stopped after the short page", page)
			w.Write([]byte(`[]`))
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &PlamoDX{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/product-category/gunpla/",
	}

	sets, err := p.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(sets) != plamodxPageLimit+1 {
		t.Fatalf("got %d sets, want %d", len(sets), plamodxPageLimit+1)
	}
}

func TestPlamoDX_FetchAll_NoProductsIsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wc/store/v1/products", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &PlamoDX{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/product-category/gunpla/",
	}

	if _, err := p.FetchAll(context.Background()); err == nil {
		t.Fatal("expected error when no products found in any category, got nil")
	}
}

func TestPlamoDX_FetchAll_RejectsUnexpectedMinorUnit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wp-json/wc/store/v1/products", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("category") != "master-grade-mg" {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`[{"id": 1, "name": "Odd", "permalink": "https://plamodx.nl/product/odd/", "is_in_stock": true, "prices": {"price": "9200", "currency_minor_unit": 3}}]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &PlamoDX{
		fetcher: httpFetcher{httpClient: srv.Client(), userAgent: "test"},
		host:    srv.URL,
		baseURL: srv.URL + "/product-category/gunpla/",
	}

	if _, err := p.FetchAll(context.Background()); err == nil {
		t.Fatal("expected error for unsupported currency_minor_unit, got nil")
	}
}
