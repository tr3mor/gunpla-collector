package reporter

import (
	"fmt"
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

// markdownEscaper escapes Telegram legacy Markdown's special characters in
// text we don't control (scraped set names), so a stray "_", "*", "`" or "["
// can't break entity parsing and cause Telegram to reject the whole message.
var markdownEscaper = strings.NewReplacer(
	"_", "\\_",
	"*", "\\*",
	"`", "\\`",
	"[", "\\[",
)

// FormatBaseline formats the "first-ever run, nothing to compare yet"
// message (spec §5 step 1).
func FormatBaseline(shopName string, setsFound int) string {
	return fmt.Sprintf("📦 *%s* — baseline collected\n\n%d sets tracked. No comparison yet — check back tomorrow.",
		shopName, setsFound)
}

// FormatFailure formats the "latest collect run failed (or appears stuck)"
// alert. Sent on every report run until a collect succeeds again — a
// scrape failure needs to keep being noticed, not fade into a one-time
// log line nobody reads.
func FormatFailure(shopName, reason string) string {
	return fmt.Sprintf("⚠️ *%s* — collect failed\n%s", markdownEscaper.Replace(shopName), markdownEscaper.Replace(truncate(reason, 500)))
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

// FormatDiff formats the daily new/removed/price-change report (spec §5
// steps 2-6). Sections with nothing to show are skipped; if there's nothing
// at all, a short "no changes" message is returned.
func FormatDiff(shopName string, newSets, removedSets []store.ReportItem, changes []store.PriceChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 *%s* — daily report\n", shopName)

	if len(newSets) == 0 && len(removedSets) == 0 && len(changes) == 0 {
		b.WriteString("\nNo changes today.")
		return b.String()
	}

	if len(newSets) > 0 {
		b.WriteString("\n🆕 *New*\n")
		for _, s := range newSets {
			fmt.Fprintf(&b, "• %s — %s\n", markdownEscaper.Replace(s.Name), formatPrice(s.PriceCents, s.Currency))
		}
	}

	if len(removedSets) > 0 {
		b.WriteString("\n❌ *Removed*\n")
		for _, s := range removedSets {
			fmt.Fprintf(&b, "• %s — last seen %s\n", markdownEscaper.Replace(s.Name), formatPrice(s.PriceCents, s.Currency))
		}
	}

	if len(changes) > 0 {
		b.WriteString("\n💰 *Price changes*\n")
		for _, c := range changes {
			pctStr := "n/a"
			if c.OldCents != 0 {
				pct := float64(c.NewCents-c.OldCents) / float64(c.OldCents) * 100
				sign := ""
				if pct > 0 {
					sign = "+"
				}
				pctStr = fmt.Sprintf("%s%.1f%%", sign, pct)
			}
			fmt.Fprintf(&b, "• %s — %s → %s (%s)\n", markdownEscaper.Replace(c.Name), formatPrice(c.OldCents, c.Currency), formatPrice(c.NewCents, c.Currency), pctStr)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
