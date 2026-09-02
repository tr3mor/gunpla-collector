package scraper

import "testing"

func TestCentsFromDecimal(t *testing.T) {
	cases := []struct {
		euros float64
		want  int
	}{
		{54.99, 5499},
		{0, 0},
		{1234.56, 123456},
		{42.9, 4290},
		{9.999, 1000}, // rounds to nearest cent
	}
	for _, c := range cases {
		if got := CentsFromDecimal(c.euros); got != c.want {
			t.Errorf("CentsFromDecimal(%v) = %d, want %d", c.euros, got, c.want)
		}
	}
}

func TestParseEuroPriceString(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"€54,99", 5499, false},
		{"54,99", 5499, false},
		{"€1.234,56", 123456, false},
		{"€0,50", 50, false},
		{"  €42,99  ", 4299, false},
		{"", 0, true},
		{"not a price", 0, true},
		{"€-5,00", 0, true},
	}
	for _, c := range cases {
		got, err := ParseEuroPriceString(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseEuroPriceString(%q) expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseEuroPriceString(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseEuroPriceString(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
