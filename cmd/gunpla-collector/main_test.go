package main

import (
	"testing"
	"time"
)

func TestRunTimeout(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		override time.Duration
		want     time.Duration
	}{
		{"collect default", "collect", 0, defaultCollectTimeout},
		{"report default", "report", 0, defaultReportTimeout},
		{"override wins for collect", "collect", 10 * time.Minute, 10 * time.Minute},
		{"override wins for report", "report", 10 * time.Minute, 10 * time.Minute},
		{"unknown cmd gets collect default", "bogus", 0, defaultCollectTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runTimeout(c.cmd, c.override); got != c.want {
				t.Errorf("runTimeout(%q, %v) = %v, want %v", c.cmd, c.override, got, c.want)
			}
		})
	}
}
