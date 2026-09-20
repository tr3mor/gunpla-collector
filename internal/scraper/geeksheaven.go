package scraper

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	geeksHeavenSlug    = "geeksheaven"
	geeksHeavenName    = "GeeksHeaven"
	geeksHeavenHost    = "https://www.geeksheaven.nl"
	geeksHeavenBaseURL = geeksHeavenHost + "/gundam-model-kits/"
)

// geeksHeavenGradePatterns maps subcategory title patterns to the grade
// codes we track. Only these grades are collected (see project decision:
// MG/HG/RG/PG only, not SD/NG, 30MM, Overige, accessories, or merchandise).
// Matching by title (not a hardcoded slug list) so a slug rename doesn't
// silently drop a grade; order matters ("Perfect Grade" before bare "PG").
var geeksHeavenGradePatterns = []struct {
	re    *regexp.Regexp
	grade string
}{
	{regexp.MustCompile(`(?i)perfect grade`), "PG"},
	{regexp.MustCompile(`(?i)\bmg\b`), "MG"},
	{regexp.MustCompile(`(?i)\bhg\b`), "HG"},
	{regexp.MustCompile(`(?i)\brg\b`), "RG"},
}

func classifyGrade(categoryTitle string) (grade string, ok bool) {
	for _, p := range geeksHeavenGradePatterns {
		if p.re.MatchString(categoryTitle) {
			return p.grade, true
		}
	}
	return "", false
}

type GeeksHeaven struct {
	fetcher httpFetcher
	host    string // scheme+host, overridable in tests
	baseURL string
}

func NewGeeksHeaven() *GeeksHeaven {
	return &GeeksHeaven{
		fetcher: httpFetcher{
			httpClient: &http.Client{Timeout: 20 * time.Second},
			userAgent:  defaultUserAgent,
			// Minimum wait between requests, honoring robots.txt's
			// `Crawl-delay: 2`, plus jitter on top.
			delay:  2 * time.Second,
			jitter: 500 * time.Millisecond,
		},
		host:    geeksHeavenHost,
		baseURL: geeksHeavenBaseURL,
	}
}

func (g *GeeksHeaven) ShopSlug() string { return geeksHeavenSlug }
func (g *GeeksHeaven) ShopName() string { return geeksHeavenName }
func (g *GeeksHeaven) BaseURL() string  { return geeksHeavenBaseURL }

// --- Lightspeed eCom (Shoplightspeed) JSON API response shapes ---

type indexResponse struct {
	Catalog struct {
		Categories map[string]struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"categories"`
	} `json:"catalog"`
}

type categoryPageResponse struct {
	Page     int               `json:"page"`
	Pages    int               `json:"pages"`
	Count    int               `json:"count"`
	Products []categoryProduct `json:"products"`
}

type categoryProduct struct {
	ID        int64  `json:"id"`
	SKU       string `json:"sku"`
	EAN       string `json:"ean"`
	Available bool   `json:"available"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Price     struct {
		PriceIncl float64 `json:"price_incl"`
	} `json:"price"`
}

func (g *GeeksHeaven) FetchAll(ctx context.Context) ([]ScrapedSet, error) {
	var idx indexResponse
	if err := g.fetcher.getJSON(ctx, g.baseURL+"?format=json", &idx); err != nil {
		return nil, fmt.Errorf("category index: %w", err)
	}

	type gradeCategory struct {
		url   string
		grade string
	}
	var categories []gradeCategory
	for _, cat := range idx.Catalog.Categories {
		if grade, ok := classifyGrade(cat.Title); ok {
			categories = append(categories, gradeCategory{url: cat.URL, grade: grade})
		}
	}
	if len(categories) == 0 {
		return nil, fmt.Errorf("no MG/HG/RG/PG categories found on index page — site structure may have changed")
	}
	// Deterministic order so request sequencing (and test expectations) is stable.
	sort.Slice(categories, func(i, j int) bool { return categories[i].url < categories[j].url })

	seen := map[string]ScrapedSet{}
	for _, cat := range categories {
		page := 1
		totalPages := 1
		for page <= totalPages {
			pageURL := fmt.Sprintf("%s/%s/page%d.ajax?format=json", g.host, cat.url, page)
			var resp categoryPageResponse
			if err := g.fetcher.getJSON(ctx, pageURL, &resp); err != nil {
				return nil, fmt.Errorf("category %s page %d: %w", cat.url, page, err)
			}
			totalPages = resp.Pages
			if totalPages < 1 {
				totalPages = 1
			}

			for _, p := range resp.Products {
				extID := strconv.FormatInt(p.ID, 10)
				available := p.Available
				set := ScrapedSet{
					ExternalID: extID,
					URL:        g.absoluteURL(p.URL),
					Name:       p.Title,
					Grade:      cat.grade,
					PriceCents: CentsFromDecimal(p.Price.PriceIncl),
					Currency:   "EUR",
					InStock:    &available,
				}
				seen[extID] = set // last category to see a product wins; harmless if grades overlap
			}
			page++
		}
	}

	sets := make([]ScrapedSet, 0, len(seen))
	for _, s := range seen {
		sets = append(sets, s)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].ExternalID < sets[j].ExternalID })
	return sets, nil
}

func (g *GeeksHeaven) absoluteURL(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	return g.host + "/" + u
}
