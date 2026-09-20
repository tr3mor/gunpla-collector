package main

import (
	"context"
	"errors"
	"flag"
	"path/filepath"
	"testing"
	"time"

	"gunpla-collector/internal/config"
	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
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

func TestParseFlags_ShopEqualsForm(t *testing.T) {
	flags, err := parseFlags("collect", []string{"--shop=geeksheaven"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.shop != "geeksheaven" {
		t.Errorf("shop = %q, want geeksheaven", flags.shop)
	}
}

// TestParseFlags_ShopSpaceForm: the old parser only understood "--shop=x",
// silently dropping the space-separated form.
func TestParseFlags_ShopSpaceForm(t *testing.T) {
	flags, err := parseFlags("collect", []string{"--shop", "geeksheaven"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.shop != "geeksheaven" {
		t.Errorf("shop = %q, want geeksheaven", flags.shop)
	}
}

func TestParseFlags_SingleDashAlsoWorks(t *testing.T) {
	flags, err := parseFlags("collect", []string{"-shop=geeksheaven", "-force"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.shop != "geeksheaven" || !flags.force {
		t.Errorf("flags = %+v, want shop=geeksheaven force=true", flags)
	}
}

func TestParseFlags_Force(t *testing.T) {
	flags, err := parseFlags("collect", []string{"--force"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !flags.force {
		t.Error("force = false, want true")
	}
}

func TestParseFlags_DefaultsAreZeroValues(t *testing.T) {
	flags, err := parseFlags("collect", nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.shop != "" || flags.force {
		t.Errorf("flags = %+v, want zero values", flags)
	}
}

func TestParseFlags_HelpReturnsErrHelp(t *testing.T) {
	_, err := parseFlags("collect", []string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want flag.ErrHelp", err)
	}
}

func TestParseFlags_UnknownFlagIsAnError(t *testing.T) {
	_, err := parseFlags("collect", []string{"--bogus"})
	if err == nil || errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want a non-help error", err)
	}
}

// An unknown subcommand must be rejected before touching the DB or
// network — both would fail in this test environment anyway.
func TestRun_UnknownSubcommandIsUsageError(t *testing.T) {
	if err := run([]string{"bogus"}); err == nil {
		t.Fatal("expected a usage error for an unknown subcommand, got nil")
	}
}

func TestRun_NoArgsIsUsageError(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("expected a usage error for no arguments, got nil")
	}
}

// "-h" is handled during flag parsing, before store.Open is ever called.
func TestRun_HelpReturnsNilWithoutTouchingStore(t *testing.T) {
	if err := run([]string{"collect", "-h"}); err != nil {
		t.Fatalf("run with -h: %v, want nil", err)
	}
}

// A shop active in the DB but whose scraper was removed from the binary
// must not show up, and the result must be sorted.
func TestResolveShops_FiltersToRegisteredScrapers(t *testing.T) {
	// Unique slugs so this doesn't collide with the real scrapers this
	// package's init() registers.
	const slugB = "zzz-resolve-test-b"
	const slugA = "zzz-resolve-test-a"
	const slugOrphan = "zzz-resolve-test-orphan"
	scraper.Register(&stubScraper{slug: slugB})
	scraper.Register(&stubScraper{slug: slugA})

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.GetOrCreateShop(ctx, slugB, "B", "https://example.com/b"); err != nil {
		t.Fatalf("GetOrCreateShop b: %v", err)
	}
	if _, err := db.GetOrCreateShop(ctx, slugA, "A", "https://example.com/a"); err != nil {
		t.Fatalf("GetOrCreateShop a: %v", err)
	}
	if _, err := db.GetOrCreateShop(ctx, slugOrphan, "Orphan", "https://example.com/orphan"); err != nil {
		t.Fatalf("GetOrCreateShop orphan: %v", err)
	}

	shops, err := resolveShops(ctx, db, config.Config{}, "")
	if err != nil {
		t.Fatalf("resolveShops: %v", err)
	}

	var gotSlugs []string
	for _, s := range shops {
		switch s.Slug {
		case slugA, slugB, slugOrphan:
			gotSlugs = append(gotSlugs, s.Slug)
		}
	}
	want := []string{slugA, slugB}
	if len(gotSlugs) != len(want) || gotSlugs[0] != want[0] || gotSlugs[1] != want[1] {
		t.Errorf("resolveShops slugs = %v, want %v (orphan excluded, sorted)", gotSlugs, want)
	}
}

type stubScraper struct{ slug string }

func (s *stubScraper) ShopSlug() string { return s.slug }
func (s *stubScraper) ShopName() string { return s.slug }
func (s *stubScraper) BaseURL() string  { return "https://example.com/" + s.slug }
func (s *stubScraper) FetchAll(ctx context.Context) ([]scraper.ScrapedSet, error) {
	return nil, nil
}
