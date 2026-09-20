package scraper

import (
	"context"
	"testing"
)

type registryTestScraper struct{ slug string }

func (s *registryTestScraper) ShopSlug() string { return s.slug }
func (s *registryTestScraper) ShopName() string { return s.slug }
func (s *registryTestScraper) BaseURL() string  { return "https://example.com/" + s.slug }
func (s *registryTestScraper) FetchAll(ctx context.Context) ([]ScrapedSet, error) {
	return nil, nil
}

// registry is a map, so All() needs its own sort to stay deterministic.
func TestAll_SortedBySlug(t *testing.T) {
	// Unique slugs so this doesn't collide with scrapers other tests may
	// have registered in this same binary.
	Register(&registryTestScraper{slug: "zzz-registry-test-c"})
	Register(&registryTestScraper{slug: "zzz-registry-test-a"})
	Register(&registryTestScraper{slug: "zzz-registry-test-b"})

	all := All()
	var lastTestSlug string
	sawAny := false
	for _, s := range all {
		slug := s.ShopSlug()
		if len(slug) < len("zzz-registry-test-") || slug[:len("zzz-registry-test-")] != "zzz-registry-test-" {
			continue // some other test's/registration's scraper — ignore
		}
		sawAny = true
		if lastTestSlug != "" && slug < lastTestSlug {
			t.Fatalf("All() not sorted: %q came after %q", slug, lastTestSlug)
		}
		lastTestSlug = slug
	}
	if !sawAny {
		t.Fatal("expected to find the registered test scrapers in All()")
	}
}

func TestGet_ReturnsRegisteredScraperBySlug(t *testing.T) {
	Register(&registryTestScraper{slug: "zzz-registry-test-get"})

	got, ok := Get("zzz-registry-test-get")
	if !ok {
		t.Fatal("Get: expected ok=true for a registered slug")
	}
	if got.ShopSlug() != "zzz-registry-test-get" {
		t.Errorf("Get returned slug %q, want zzz-registry-test-get", got.ShopSlug())
	}

	if _, ok := Get("zzz-registry-test-does-not-exist"); ok {
		t.Error("Get: expected ok=false for an unregistered slug")
	}
}
