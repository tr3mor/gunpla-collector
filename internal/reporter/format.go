package reporter

import (
	"fmt"
	"html"
	"strings"

	"gunpla-collector/internal/store"
)

func formatPrice(cents int, currency string) string {
	return fmt.Sprintf("%s%.2f", currencySymbol(currency), float64(cents)/100)
}

// currencySymbol maps an ISO 4217 code to the symbol shown in reports.
// Shops we don't have a symbol for still render legibly, e.g. "DKK 92.00".
func currencySymbol(currency string) string {
	switch currency {
	case "EUR":
		return "€"
	case "USD":
		return "$"
	case "GBP":
		return "£"
	default:
		return currency + " "
	}
}

// esc escapes text we don't control (set names, shop name) for Telegram's
// HTML parse mode, so a stray "<", ">" or "&" can't break the message.
func esc(s string) string {
	return html.EscapeString(s)
}

// FormatBaseline formats the "first-ever run, nothing to compare yet"
// message.
func FormatBaseline(shopName string, setsFound int) string {
	return fmt.Sprintf("📦 <b>%s</b> — baseline collected\n\n%d sets tracked. No comparison yet — check back tomorrow.",
		esc(shopName), setsFound)
}

// FormatFailure formats the "collect failed (or appears stuck)" alert,
// sent on every report run until a collect succeeds again.
func FormatFailure(shopName, reason string) string {
	return fmt.Sprintf("⚠️ <b>%s</b> — collect failed\n%s", esc(shopName), esc(truncate(reason, 500)))
}

// truncate shortens s to at most n runes, appending an ellipsis if it was
// cut. Used to keep scraper error text (which may embed a URL or response
// body) from blowing past Telegram's message limit on its own.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// FormatDiff formats the daily new/removed/price-change report. Sections
// with nothing to show are skipped; if there's nothing at all, a short
// "no changes" message is returned.
func FormatDiff(shopName string, newSets, removedSets []store.ReportItem, changes []store.PriceChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 <b>%s</b> — daily report\n", esc(shopName))

	if len(newSets) == 0 && len(removedSets) == 0 && len(changes) == 0 {
		b.WriteString("\nNo changes today.")
		return b.String()
	}

	if len(newSets) > 0 {
		b.WriteString("\n🆕 <b>New</b>\n")
		for _, s := range newSets {
			fmt.Fprintf(&b, "• %s — %s\n", esc(s.Name), formatPrice(s.PriceCents, s.Currency))
		}
	}

	if len(removedSets) > 0 {
		b.WriteString("\n❌ <b>Removed</b>\n")
		for _, s := range removedSets {
			fmt.Fprintf(&b, "• %s — last seen %s\n", esc(s.Name), formatPrice(s.PriceCents, s.Currency))
		}
	}

	if len(changes) > 0 {
		b.WriteString("\n💰 <b>Price changes</b>\n")
		for _, c := range changes {
			pctStr := "n/a"
			if pct, ok := pricePctChange(c.OldCents, c.NewCents); ok {
				sign := ""
				if pct > 0 {
					sign = "+"
				}
				pctStr = fmt.Sprintf("%s%.1f%%", sign, pct)
			}
			fmt.Fprintf(&b, "• %s — %s → %s (%s)\n", esc(c.Name), formatPrice(c.OldCents, c.Currency), formatPrice(c.NewCents, c.Currency), pctStr)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// pricePctChange returns the percentage change from oldCents to newCents.
// ok is false when oldCents is zero, since the percentage is undefined then.
func pricePctChange(oldCents, newCents int) (pct float64, ok bool) {
	if oldCents == 0 {
		return 0, false
	}
	return float64(newCents-oldCents) / float64(oldCents) * 100, true
}
