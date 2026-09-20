package config

import (
	"testing"
	"time"
)

func TestParseShops(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"geeksheaven", []string{"geeksheaven"}},
		{"geeksheaven,gundamstore", []string{"geeksheaven", "gundamstore"}},
		{" geeksheaven , gundamstore ", []string{"geeksheaven", "gundamstore"}},
		{"geeksheaven,,gundamstore", []string{"geeksheaven", "gundamstore"}},
		{"geeksheaven,", []string{"geeksheaven"}},
	}
	for _, c := range cases {
		got := parseShops(c.raw)
		if !equalStrings(got, c.want) {
			t.Errorf("parseShops(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseRunTimeout(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", 0},
		{"   ", 0},
		{"45m", 45 * time.Minute},
		{"1h", time.Hour},
		{"not-a-duration", 0}, // malformed falls back to 0 (command default), not an error
		{"-5m", 0},            // non-positive is rejected, same reasoning
		{"0s", 0},
	}
	for _, c := range cases {
		got := parseRunTimeout(c.raw)
		if got != c.want {
			t.Errorf("parseRunTimeout(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestParseBool(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"1", true},
		{"0", false},
		{"true", true},
		{"TRUE", true},
		{"false", false},
		{"  true  ", true},
		{"not-a-bool", false}, // malformed falls back to false, not an error
		{"yes", false},        // strconv.ParseBool doesn't accept "yes"/"no"
	}
	for _, c := range cases {
		got := parseBool(c.raw)
		if got != c.want {
			t.Errorf("parseBool(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestValidateForReport(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"missing both", Config{}, true},
		{"missing chat id", Config{TelegramToken: "t"}, true},
		{"missing token", Config{TelegramChatID: "c"}, true},
		{"both set", Config{TelegramToken: "t", TelegramChatID: "c"}, false},
	}
	for _, c := range cases {
		err := c.cfg.ValidateForReport()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: ValidateForReport() error = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}
