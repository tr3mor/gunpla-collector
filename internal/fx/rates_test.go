package fx

import "testing"

func TestToEUR(t *testing.T) {
	r := Rates{"DKK": 0.9}
	tests := []struct {
		name     string
		cents    int
		currency string
		want     int
		wantRate float64
		wantOK   bool
	}{
		{"converts", 10000, "DKK", 9000, 0.9, true},
		{"rounds", 1001, "DKK", 901, 0.9, true},
		{"EUR passes through", 500, "EUR", 500, 1, true},
		{"no rate", 500, "JPY", 0, 0, false},
	}
	for _, tt := range tests {
		got, rate, ok := r.ToEUR(tt.cents, tt.currency)
		if got != tt.want || rate != tt.wantRate || ok != tt.wantOK {
			t.Errorf("%s: got %d, %v, %v; want %d, %v, %v", tt.name, got, rate, ok, tt.want, tt.wantRate, tt.wantOK)
		}
	}
	var none Rates
	if _, _, ok := none.ToEUR(500, "DKK"); ok {
		t.Error("nil rates should not convert USD")
	}
	if got, _, ok := none.ToEUR(500, "EUR"); !ok || got != 500 {
		t.Errorf("nil rates, EUR: got %d, %v", got, ok)
	}
}
