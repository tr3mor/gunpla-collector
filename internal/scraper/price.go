package scraper

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CentsFromDecimal converts a decimal major-unit amount (e.g. from a JSON
// API's float or numeric-string price field, in whatever currency the shop
// uses) to integer minor units ("cents"), rounding to the nearest cent.
// Currency-agnostic — the caller tracks which currency the amount is in.
func CentsFromDecimal(amount float64) int {
	return int(math.Round(amount * 100))
}

// ParseEuroPriceString parses a Dutch-formatted euro price string such as
// "€1.234,56" or "54,99" (thousands separator ".", decimal separator ",")
// into integer cents. Kept for HTML-scraping shops that expose price only
// as display text.
//
// TODO: currently has no production caller (every registered scraper reads
// price as a JSON number via CentsFromDecimal) — wire it up when the first
// HTML-only shop scraper is added, or remove it if that never happens.
func ParseEuroPriceString(s string) (int, error) {
	orig := s
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "€")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty price string %q", orig)
	}

	// Thousands separator "." (if present) then decimal separator ",".
	s = strings.ReplaceAll(s, ".", "")
	s = strings.Replace(s, ",", ".", 1)

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse price %q: %w", orig, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("negative price %q", orig)
	}
	return CentsFromDecimal(f), nil
}

// centsFromPriceString parses a plain decimal price string (e.g. Shopify's
// variant.price, "92.00") into integer cents. Unlike ParseEuroPriceString,
// there's no locale formatting to undo.
func centsFromPriceString(s string) (int, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("parse price %q: %w", s, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("negative price %q", s)
	}
	return CentsFromDecimal(f), nil
}
