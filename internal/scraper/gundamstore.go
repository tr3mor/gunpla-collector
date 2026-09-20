package scraper

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const (
	gundamStoreSlug     = "gundamstore"
	gundamStoreName     = "Gundam Store"
	gundamStoreHost     = "https://gundam-store.com"
	gundamStoreBaseURL  = gundamStoreHost + "/collections/mg-master-grade"
	gundamStoreCurrency = "USD"
	// Shopify's products.json endpoint caps out at 250 items per page.
	gundamStorePageLimit = 250
)

// gundamStoreGradeCollections maps the grade codes we track (see project
// decision: MG/HG/RG/PG only, matching GeeksHeaven) to this store's Shopify
// collection handles. gundam-store.com is a general hobby store (1200+
// collections total, not Gundam-only), and the handles don't follow a
// predictable "<code>-<name>" pattern — Perfect Grade is "perfect-grade",
// not "pg-perfect-grade" — so these were confirmed by hand against
// /collections.json rather than derived from the grade code.
var gundamStoreGradeCollections = []struct {
	handle string
	grade  string
}{
	{"mg-master-grade", "MG"},
	{"hg-high-grade", "HG"},
	{"rg-real-grade", "RG"},
	{"perfect-grade", "PG"},
}

type GundamStore struct {
	fetcher httpFetcher
	host    string // scheme+host, overridable in tests
	baseURL string
}

func NewGundamStore() *GundamStore {
	return &GundamStore{
		fetcher: httpFetcher{
			httpClient: &http.Client{Timeout: 20 * time.Second},
			userAgent:  defaultUserAgent,
			// robots.txt specifies no Crawl-delay for this store, so this
			// is a conservative self-imposed choice rather than one
			// derived from site policy.
			delay:  time.Second,
			jitter: 300 * time.Millisecond,
		},
		host:    gundamStoreHost,
		baseURL: gundamStoreBaseURL,
	}
}

func (g *GundamStore) ShopSlug() string { return gundamStoreSlug }
func (g *GundamStore) ShopName() string { return gundamStoreName }
func (g *GundamStore) BaseURL() string  { return g.baseURL }

// --- Shopify storefront products.json response shapes ---
// (https://shopify.dev/docs/api/ajax/reference/product — the same JSON a
// collection page's theme fetches client-side; no auth required.)

type shopifyProductsResponse struct {
	Products []shopifyProduct `json:"products"`
}

type shopifyProduct struct {
	ID       int64            `json:"id"`
	Title    string           `json:"title"`
	Handle   string           `json:"handle"`
	Variants []shopifyVariant `json:"variants"`
}

type shopifyVariant struct {
	Available bool   `json:"available"`
	Price     string `json:"price"`   // decimal string, e.g. "92.00"
	SKU       string `json:"sku"`     // merchant-assigned, may be blank
	Barcode   string `json:"barcode"` // typically EAN/UPC when set at all
}

func (g *GundamStore) FetchAll(ctx context.Context) ([]ScrapedSet, error) {
	seen := map[string]ScrapedSet{}

	for _, cat := range gundamStoreGradeCollections {
		for page := 1; ; page++ {
			pageURL := fmt.Sprintf("%s/collections/%s/products.json?limit=%d&page=%d", g.host, cat.handle, gundamStorePageLimit, page)
			var resp shopifyProductsResponse
			if err := g.fetcher.getJSON(ctx, pageURL, &resp); err != nil {
				return nil, fmt.Errorf("collection %s page %d: %w", cat.handle, page, err)
			}
			if len(resp.Products) == 0 {
				break
			}

			for _, p := range resp.Products {
				if len(p.Variants) == 0 {
					continue
				}
				v := p.Variants[0]
				priceCents, err := centsFromPriceString(v.Price)
				if err != nil {
					return nil, fmt.Errorf("parse price for product %d (%s): %w", p.ID, p.Handle, err)
				}
				extID := strconv.FormatInt(p.ID, 10)
				available := v.Available
				set := ScrapedSet{
					ExternalID: extID,
					URL:        g.host + "/products/" + p.Handle,
					Name:       p.Title,
					Grade:      cat.grade,
					PriceCents: priceCents,
					Currency:   gundamStoreCurrency,
					InStock:    &available,
					EAN:        v.Barcode,
					SKU:        v.SKU,
				}
				seen[extID] = set // last collection to see a product wins; harmless if grades overlap
			}

			if len(resp.Products) < gundamStorePageLimit {
				break // short page: this was the last one
			}
		}
	}

	if len(seen) == 0 {
		return nil, fmt.Errorf("no products found across MG/HG/RG/PG collections — site structure may have changed")
	}

	sets := make([]ScrapedSet, 0, len(seen))
	for _, s := range seen {
		sets = append(sets, s)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].ExternalID < sets[j].ExternalID })
	return sets, nil
}
