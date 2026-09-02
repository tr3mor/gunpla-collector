package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	gundamStoreSlug     = "gundamstore"
	gundamStoreName     = "Gundam Store"
	gundamStoreHost     = "https://gundam-store.com"
	gundamStoreBaseURL  = gundamStoreHost + "/collections/mg-master-grade"
	gundamStoreUA       = "gunpla-collector/1.0 (+https://github.com/; contact via shop enquiry form)"
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
	httpClient *http.Client
	host       string // scheme+host, overridable in tests
	baseURL    string
	userAgent  string
	// delay is the minimum wait between requests. robots.txt specifies no
	// Crawl-delay for this store, so this is a conservative self-imposed
	// choice rather than one derived from site policy.
	delay time.Duration
}

func NewGundamStore() *GundamStore {
	return &GundamStore{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		host:       gundamStoreHost,
		baseURL:    gundamStoreBaseURL,
		userAgent:  gundamStoreUA,
		delay:      time.Second,
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
	Price     string `json:"price"` // decimal string, e.g. "92.00"
}

func (g *GundamStore) FetchAll(ctx context.Context) ([]ScrapedSet, error) {
	seen := map[string]ScrapedSet{}
	first := true

	for _, cat := range gundamStoreGradeCollections {
		for page := 1; ; page++ {
			if !first {
				g.sleep()
			}
			first = false

			pageURL := fmt.Sprintf("%s/collections/%s/products.json?limit=%d&page=%d", g.host, cat.handle, gundamStorePageLimit, page)
			body, err := g.get(ctx, pageURL)
			if err != nil {
				return nil, fmt.Errorf("fetch %s page %d: %w", cat.handle, page, err)
			}
			var resp shopifyProductsResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return nil, fmt.Errorf("parse %s page %d: %w", cat.handle, page, err)
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

func centsFromPriceString(s string) (int, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("parse price %q: %w", s, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("negative price %q", s)
	}
	return CentsFromDecimal(f), nil
}

func (g *GundamStore) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", g.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func (g *GundamStore) sleep() {
	jitter := time.Duration(rand.Intn(300)) * time.Millisecond
	time.Sleep(g.delay + jitter)
}
