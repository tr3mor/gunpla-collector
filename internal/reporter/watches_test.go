package reporter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
	"gunpla-collector/internal/watchlist"
)

type captureSender struct {
	sent []string
	err  error
}

func (c *captureSender) SendMessage(ctx context.Context, text string) error {
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, text)
	return nil
}

func TestFormatWatchAlerts(t *testing.T) {
	best := watchlist.Match{Listing: watchlist.Listing{HasEUR: true, EURCents: 4800}}
	best.ShopName, best.URL, best.Currency, best.CurrentCents = "Zeon <Market>", "https://z/x", "USD", 5400
	got := FormatWatchAlerts([]watchlist.Alert{{
		Watch:   watchlist.Watch{Label: "RG Nu & friends", MaxPriceCents: 5000},
		Reasons: []string{watchlist.ReasonTarget, watchlist.ReasonNew},
		Best:    best, HasBest: true,
		NewListings: []watchlist.Match{{Listing: watchlist.Listing{Name: "RG Nu", ShopName: "Shop", URL: "https://s/nu", Currency: "EUR", CurrentCents: 5100, Availability: scraper.AvailabilityPreorder, HasEUR: true, EURCents: 5100}}},
	}})
	for _, want := range []string{
		"RG Nu &amp; friends", "under your €50.00 target", `<a href="https://z/x">Zeon &lt;Market&gt;</a>`, "€48.00 ($54.00)",
		"1 new listing", `<a href="https://s/nu">RG Nu</a> — Shop, €51.00 · pre-order`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message missing %q:\n%s", want, got)
		}
	}
}

func TestRunWatches_SendsOnceAndRetriesAFailedSend(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	shop, _ := s.GetOrCreateShop(ctx, "a", "A", "https://a")
	run, _ := s.StartRun(ctx, shop.ID, "2026-01-01T00:00:00Z")
	if err := s.ApplyRun(ctx, shop.ID, run, []scraper.ScrapedSet{{ExternalID: "1", URL: "https://a/1", Name: "MG RX-78-2 Gundam Ver.Ka", Grade: "MG", PriceCents: 4000, Currency: "EUR", Availability: scraper.AvailabilityInStock}}, "2026-01-01T00:00:00Z", nil); err != nil {
		t.Fatal(err)
	}
	wl := watchlist.New(s)
	if _, err := wl.Save(ctx, watchlist.Watch{Label: "Ka", Enabled: true, MaxPriceCents: 5000,
		Spec: watchlist.Spec{Grade: "MG", Model: "rx782", Variants: []string{"verka"}}}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	failing := &captureSender{err: errors.New("telegram down")}
	if err := RunWatches(ctx, wl, failing, logger); err == nil {
		t.Fatal("a failed send must be an error")
	}
	ok := &captureSender{}
	if err := RunWatches(ctx, wl, ok, logger); err != nil || len(ok.sent) != 1 {
		t.Fatalf("retry: err %v, sent %d, want the alert sent after the failure", err, len(ok.sent))
	}
	if err := RunWatches(ctx, wl, ok, logger); err != nil || len(ok.sent) != 1 {
		t.Fatalf("repeat run: err %v, sent %d, want nothing new", err, len(ok.sent))
	}
}
