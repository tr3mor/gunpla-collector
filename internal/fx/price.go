package fx

import "math"

// Price is an amount as shown to the user: in EUR when a rate exists,
// otherwise in the shop's own currency.
type Price struct {
	Cents    int
	Currency string
	// OriginalCents/OriginalCurrency hold the shop's own price when it was
	// converted to EUR; zero/empty otherwise.
	OriginalCents    int
	OriginalCurrency string
}

// Display presents cents in currency. A currency with a rate is converted
// to EUR and the original is kept; EUR and currencies with no rate pass
// through unchanged. Amounts in one currency always convert alike, so a
// price and its history never end up in mixed currencies.
func (r Rates) Display(cents int, currency string) Price {
	eur, ok := r.convert(cents, currency)
	if !ok || currency == "EUR" {
		return Price{Cents: cents, Currency: currency}
	}
	return Price{Cents: eur, Currency: "EUR", OriginalCents: cents, OriginalCurrency: currency}
}

// Compare returns cents as EUR cents for cross-shop comparison. ok is false
// when the currency has no rate: such a price isn't comparable, which is
// exactly when Display leaves it unconverted.
func (r Rates) Compare(cents int, currency string) (eurCents int, ok bool) {
	return r.convert(cents, currency)
}

func (r Rates) convert(cents int, currency string) (int, bool) {
	if currency == "EUR" {
		return cents, true
	}
	rate, found := r[currency]
	if !found {
		return cents, false
	}
	return int(math.Round(float64(cents) * rate)), true
}

// Offer is one listing's price for Cheapest.
type Offer struct {
	Cents    int
	Currency string
	// Excluded offers (e.g. out of stock) can't win.
	Excluded bool
}

// Cheapest flags the lowest-priced offer(s) in EUR, tied offers all
// flagged. Excluded offers and those in a currency with no rate can't win,
// matching what Display shows: only amounts the user sees in EUR are
// compared. Nothing is flagged when no offer is comparable.
func (r Rates) Cheapest(offers []Offer) []bool {
	eur := make([]int, len(offers))
	best, found := 0, false
	for i, o := range offers {
		eur[i] = -1 // not a candidate
		if o.Excluded {
			continue
		}
		cents, ok := r.Compare(o.Cents, o.Currency)
		if !ok {
			continue
		}
		eur[i] = cents
		if !found || cents < best {
			best, found = cents, true
		}
	}
	flags := make([]bool, len(offers))
	for i := range offers {
		flags[i] = found && eur[i] == best
	}
	return flags
}
