package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func zeonCard(href, title, price, stockClass, stockText string) string {
	return fmt.Sprintf(`<div class="product-card product-card--view-1 border">
<form><a href="%[1]s"><img alt="x"></a>
<div class="product-card__info"><a class="product-card__title h4 block-link no-hover" href="%[1]s">%[2]s</a>
<div class="product-card__stock white-space--nowrap">
	<span class="%[4]s">%[5]s</span>
</div>
<div class="product-card__price "><span class="product-card__price--sell text-regular">%[3]s</span></div>
</div></form></div>`, href, title, price, stockClass, stockText)
}

func TestZeonMarket_FetchAll(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/MG", func(w http.ResponseWriter, r *http.Request) {
		var body string
		switch r.URL.Query().Get("page") {
		case "1":
			body = zeonCard("https://z.test/mg-one", "1/100 MG One &amp; Co", "€ 94,95 *", "success", "Op voorraad") +
				zeonCard("https://z.test/mg-two", "1/100 MG Two", "€ 1.129,00 *", "error", "Niet op voorraad")
		case "2":
			body = zeonCard("https://z.test/mg-three", "1/100 MG Three", "€ 50,00 *", "warning", "Beperkt op voorraad")
		}
		w.Write([]byte("<html>" + body + "</html>"))
	})
	mux.HandleFunc("/HG", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Write([]byte(zeonCard("https://z.test/hg", "HG Four", "€ 19,95", "success", "Op voorraad")))
		}
	})
	mux.HandleFunc("/RG", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Write([]byte(zeonCard("https://z.test/rg", "RG Five", "€ 39,95", "success", "Op voorraad")))
		}
	})
	mux.HandleFunc("/PG", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			w.Write([]byte(zeonCard("https://z.test/pg", "PG Six", "€ 249,95", "error", "Niet op voorraad")))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	z := NewZeonMarket()
	z.host = srv.URL
	z.fetcher.delay, z.fetcher.jitter = 0, 0

	sets, err := z.FetchAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 6 {
		t.Fatalf("got %d sets, want 6: %+v", len(sets), sets)
	}
	byID := map[string]ScrapedSet{}
	for _, s := range sets {
		byID[s.ExternalID] = s
	}
	one := byID["https://z.test/mg-one"]
	if one.ExternalID != "https://z.test/mg-one" || one.Name != "1/100 MG One & Co" || one.Grade != "MG" ||
		one.PriceCents != 9495 || one.Currency != "EUR" || one.URL != "https://z.test/mg-one" ||
		one.InStock == nil || !*one.InStock {
		t.Errorf("unexpected set 1: %+v", one)
	}
	two := byID["https://z.test/mg-two"]
	if two.PriceCents != 112900 || two.InStock == nil || *two.InStock {
		t.Errorf("unexpected set 2: %+v", two)
	}
	three := byID["https://z.test/mg-three"]
	if three.InStock == nil || !*three.InStock {
		t.Errorf("limited stock should count as in stock: %+v", three)
	}
	for id, want := range map[string]string{"https://z.test/hg": "HG", "https://z.test/rg": "RG", "https://z.test/pg": "PG"} {
		if byID[id].Grade != want {
			t.Errorf("%s grade = %q, want %q", id, byID[id].Grade, want)
		}
	}
}

func TestZeonMarket_EmptyFirstPageIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()
	z := NewZeonMarket()
	z.host = srv.URL
	z.fetcher.delay, z.fetcher.jitter = 0, 0
	if _, err := z.FetchAll(context.Background()); err == nil {
		t.Fatal("expected error for empty first page")
	}
}
