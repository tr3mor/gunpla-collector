package fx

import (
	"slices"
	"testing"
)

func TestDisplay(t *testing.T) {
	r := Rates{"USD": 0.9}
	tests := []struct {
		name     string
		cents    int
		currency string
		want     Price
	}{
		{"converted, original kept", 10000, "USD", Price{9000, "EUR", 10000, "USD"}},
		{"rounds", 333, "USD", Price{300, "EUR", 333, "USD"}}, // 299.7
		{"EUR passes through", 500, "EUR", Price{Cents: 500, Currency: "EUR"}},
		{"no rate passes through", 500, "GBP", Price{Cents: 500, Currency: "GBP"}},
	}
	for _, tt := range tests {
		if got := r.Display(tt.cents, tt.currency); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
	// A stray EUR entry never "converts" EUR.
	if got := (Rates{"EUR": 2}).Display(500, "EUR"); got != (Price{Cents: 500, Currency: "EUR"}) {
		t.Errorf("EUR entry: got %+v", got)
	}
	var none Rates
	if got := none.Display(500, "USD"); got != (Price{Cents: 500, Currency: "USD"}) {
		t.Errorf("nil rates: got %+v", got)
	}
}

func TestCompare(t *testing.T) {
	r := Rates{"USD": 0.9}
	if got, ok := r.Compare(10000, "USD"); !ok || got != 9000 {
		t.Errorf("USD: got %d, %v", got, ok)
	}
	if got, ok := r.Compare(500, "EUR"); !ok || got != 500 {
		t.Errorf("EUR: got %d, %v", got, ok)
	}
	if _, ok := r.Compare(500, "JPY"); ok {
		t.Error("JPY has no rate, should not be comparable")
	}
}

func TestCheapest(t *testing.T) {
	r := Rates{"USD": 0.9}
	tests := []struct {
		name   string
		offers []Offer
		want   []bool
	}{
		{"compares across currencies", []Offer{{Cents: 10000, Currency: "USD"}, {Cents: 9500, Currency: "EUR"}}, []bool{true, false}},
		{"ties all win", []Offer{{Cents: 9000, Currency: "EUR"}, {Cents: 10000, Currency: "USD"}}, []bool{true, true}},
		{"excluded never wins", []Offer{{Cents: 100, Currency: "EUR", Excluded: true}, {Cents: 900, Currency: "EUR"}}, []bool{false, true}},
		{"no rate is skipped", []Offer{{Cents: 1, Currency: "JPY"}, {Cents: 9999, Currency: "EUR"}}, []bool{false, true}},
		{"nothing comparable", []Offer{{Cents: 1, Currency: "JPY"}, {Cents: 5, Currency: "GBP"}}, []bool{false, false}},
		{"empty", nil, []bool{}},
	}
	for _, tt := range tests {
		if got := r.Cheapest(tt.offers); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
