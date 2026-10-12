package reporter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/telegram"
	"gunpla-collector/internal/watchlist"
)

// maxNewListingsShown caps the new listings named under one watch.
const maxNewListingsShown = 5

// WatchEvaluator is what RunWatches needs from the watchlist.
type WatchEvaluator interface {
	Evaluate(ctx context.Context) (*watchlist.Pass, error)
}

// RunWatches sends one Telegram message with every watch that has news, then
// records what it saw. State is stored only after the send succeeded, so a
// failed send is retried by the next run instead of being lost; and running
// it twice in a row sends nothing the second time.
func RunWatches(ctx context.Context, ev WatchEvaluator, sender telegram.Sender, logger *slog.Logger) error {
	pass, err := ev.Evaluate(ctx)
	if err != nil {
		return fmt.Errorf("evaluate watches: %w", err)
	}
	if len(pass.Alerts) > 0 {
		logger.Info("sending watch alerts", "alerts", len(pass.Alerts))
		if err := telegram.SendLong(ctx, sender, FormatWatchAlerts(pass.Alerts)); err != nil {
			return fmt.Errorf("send watch alerts: %w", err)
		}
	}
	if err := pass.Commit(ctx); err != nil {
		return fmt.Errorf("save watch state: %w", err)
	}
	return nil
}

// FormatWatchAlerts formats the watchlist message.
func FormatWatchAlerts(alerts []watchlist.Alert) string {
	var b strings.Builder
	b.WriteString("🎯 <b>Watchlist</b>\n")
	for _, a := range alerts {
		fmt.Fprintf(&b, "\n<b>%s</b>\n", esc(a.Watch.Label))
		for _, reason := range a.Reasons {
			switch reason {
			case watchlist.ReasonTarget:
				fmt.Fprintf(&b, "• under your %s target: %s\n", formatPrice(a.Watch.MaxPriceCents, "EUR"), offer(a.Best))
			case watchlist.ReasonRestock:
				fmt.Fprintf(&b, "• back in stock: %s\n", offer(a.Best))
			case watchlist.ReasonNew:
				n := len(a.NewListings)
				fmt.Fprintf(&b, "• %d new listing%s\n", n, plural(n))
				for i, m := range a.NewListings {
					if i == maxNewListingsShown {
						fmt.Fprintf(&b, "  … and %d more\n", n-maxNewListingsShown)
						break
					}
					fmt.Fprintf(&b, "  %s — %s, %s\n", link(m.Name, m.URL), esc(m.ShopName), priceNote(m))
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// offer renders a listing as "<shop link> — €48.00".
func offer(m watchlist.Match) string {
	return fmt.Sprintf("%s — %s", link(m.ShopName, m.URL), priceNote(m))
}

// priceNote is the listing's EUR price, with the shop's own price when it
// was converted, and a pre-order or out-of-stock note.
func priceNote(m watchlist.Match) string {
	s := "price unknown"
	if m.HasEUR {
		s = formatPrice(m.EURCents, "EUR")
		if m.Currency != "EUR" {
			s += " (" + formatPrice(m.CurrentCents, m.Currency) + ")"
		}
	}
	switch m.Availability {
	case scraper.AvailabilityPreorder:
		s += " · pre-order"
	case scraper.AvailabilityOutOfStock:
		s += " · out of stock"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
