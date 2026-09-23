package scraper

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const (
	plamodxSlug     = "plamodx"
	plamodxName     = "PlamoDX"
	plamodxHost     = "https://plamodx.nl"
	plamodxBaseURL  = plamodxHost + "/product-category/gunpla/"
	plamodxCurrency = "EUR"
	// WooCommerce's Store API caps per_page at 100; a higher value returns
	// a 400.
	plamodxPageLimit = 100
)

// plamodxGradeCategories maps the grade codes we track (see project
// decision: MG/HG/RG/PG only, matching GeeksHeaven/Gundam Store) to this
// store's WooCommerce category slugs. plamodx.nl is a general hobby store
// with a separate "Decals" taxonomy branch that reuses the exact same
// display names ("High Grade (HG)" etc. under decals/hg, decals/mg, ...),
// so matching by category title alone (as GeeksHeaven does) would also
// pull in decal sheets. These slugs — confirmed by hand against
// /wp-json/wc/store/v1/products/categories — are the ones nested under the
// "Gunpla" category (id 225), not the "Decals" one (id 234).
var plamodxGradeCategories = []struct {
	slug  string
	grade string
}{
	{"master-grade-mg", "MG"},
	{"high-grade-hg", "HG"},
	{"real-grade-rg", "RG"},
	{"perfect-grade-pg", "PG"},
}

type PlamoDX struct {
	fetcher httpFetcher
	host    string // scheme+host, overridable in tests
	baseURL string
}

func NewPlamoDX() *PlamoDX {
	return &PlamoDX{
		fetcher: httpFetcher{
			httpClient: &http.Client{Timeout: 20 * time.Second},
			userAgent:  defaultUserAgent,
			// robots.txt specifies no Crawl-delay for this store, so this
			// is a conservative self-imposed choice rather than one
			// derived from site policy.
			delay:  time.Second,
			jitter: 300 * time.Millisecond,
		},
		host:    plamodxHost,
		baseURL: plamodxBaseURL,
	}
}

func (p *PlamoDX) ShopSlug() string { return plamodxSlug }
func (p *PlamoDX) ShopName() string { return plamodxName }
func (p *PlamoDX) BaseURL() string  { return p.baseURL }

// --- WooCommerce Store API response shapes ---
// (https://github.com/woocommerce/woocommerce/blob/trunk/plugins/woocommerce/src/StoreApi — the
// public, unauthenticated `/wp-json/wc/store/v1/products` endpoint a
// storefront's own product listing fetches client-side.)

type wcStoreProduct struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Permalink string `json:"permalink"`
	SKU       string `json:"sku"`
	IsInStock bool   `json:"is_in_stock"`
	Prices    struct {
		Price             string `json:"price"` // integer string in minor units, e.g. "3599" for €35.99
		CurrencyMinorUnit int    `json:"currency_minor_unit"`
	} `json:"prices"`
}

func (p *PlamoDX) FetchAll(ctx context.Context) ([]ScrapedSet, error) {
	seen := map[string]ScrapedSet{}

	for _, cat := range plamodxGradeCategories {
		for page := 1; ; page++ {
			pageURL := fmt.Sprintf("%s/wp-json/wc/store/v1/products?category=%s&per_page=%d&page=%d", p.host, cat.slug, plamodxPageLimit, page)
			var products []wcStoreProduct
			if err := p.fetcher.getJSON(ctx, pageURL, &products); err != nil {
				return nil, fmt.Errorf("category %s page %d: %w", cat.slug, page, err)
			}
			if len(products) == 0 {
				break
			}

			for _, prod := range products {
				priceCents, err := centsFromMinorUnitString(prod.Prices.Price, prod.Prices.CurrencyMinorUnit)
				if err != nil {
					return nil, fmt.Errorf("parse price for product %d (%s): %w", prod.ID, prod.Name, err)
				}
				extID := strconv.FormatInt(prod.ID, 10)
				inStock := prod.IsInStock
				set := ScrapedSet{
					ExternalID: extID,
					URL:        prod.Permalink,
					Name:       html.UnescapeString(prod.Name),
					Grade:      cat.grade,
					PriceCents: priceCents,
					Currency:   plamodxCurrency,
					InStock:    &inStock,
					SKU:        prod.SKU,
				}
				seen[extID] = set // last category to see a product wins; harmless if grades overlap
			}

			if len(products) < plamodxPageLimit {
				break // short page: this was the last one
			}
		}
	}

	if len(seen) == 0 {
		return nil, fmt.Errorf("no products found across MG/HG/RG/PG categories — site structure may have changed")
	}

	sets := make([]ScrapedSet, 0, len(seen))
	for _, s := range seen {
		sets = append(sets, s)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].ExternalID < sets[j].ExternalID })
	return sets, nil
}
