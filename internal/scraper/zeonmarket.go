package scraper

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	zeonMarketSlug     = "zeonmarket"
	zeonMarketName     = "Zeonmarket"
	zeonMarketHost     = "https://www.zeonmarket.nl"
	zeonMarketBaseURL  = zeonMarketHost + "/MG"
	zeonMarketCurrency = "EUR"
	// Safety stop for the page loop; MG has ~11 pages of 12 products.
	zeonMarketMaxPages = 100
)

// zeonMarketSources lists the category paths of this CCV Shop storefront
// to read. Each is server-rendered HTML, 12 products per page, paged with
// ?page=N (the same URL the site's infinite scroll pushes into the address
// bar). The grade categories fix the grade; /Pre-orders is a flat list of
// every kind of kit, so its grade is guessed from the name (or left empty)
// and each item is marked as a pre-order. It goes last so that if an item
// ever appears in both, the pre-order status wins.
var zeonMarketSources = []struct {
	path     string
	grade    string
	preorder bool
}{
	{"/MG", "MG", false},
	{"/HG", "HG", false},
	{"/RG", "RG", false},
	{"/PG", "PG", false},
	{"/Pre-orders", "", true},
}

// zeonPreorderPrefix is the "PRE-ORDER " marker the shop puts at the start
// of every pre-order title; stripped so the name reads like the released
// product's.
var zeonPreorderPrefix = regexp.MustCompile(`(?i)^pre-order\s+`)

var (
	zeonCardSplit = regexp.MustCompile(`class="product-card product-card`)
	zeonTitleRe   = regexp.MustCompile(`class="product-card__title[^"]*"\s+href="([^"]+)"[^>]*>([^<]*)<`)
	zeonPriceRe   = regexp.MustCompile(`product-card__price--sell[^>]*>([^<]*)<`)
	zeonStockRe   = regexp.MustCompile(`product-card__stock[^>]*>\s*<span class="(success|warning|error)"`)
)

type ZeonMarket struct {
	fetcher httpFetcher
	host    string // scheme+host, overridable in tests
}

func NewZeonMarket() *ZeonMarket {
	return &ZeonMarket{
		fetcher: httpFetcher{
			httpClient: &http.Client{Timeout: 20 * time.Second},
			userAgent:  defaultUserAgent,
			accept:     "text/html",
			// robots.txt specifies no Crawl-delay for this store, so this
			// is a conservative self-imposed choice rather than one
			// derived from site policy.
			delay:  time.Second,
			jitter: 300 * time.Millisecond,
		},
		host: zeonMarketHost,
	}
}

func (z *ZeonMarket) ShopSlug() string { return zeonMarketSlug }
func (z *ZeonMarket) ShopName() string { return zeonMarketName }
func (z *ZeonMarket) BaseURL() string  { return zeonMarketBaseURL }

func (z *ZeonMarket) FetchAll(ctx context.Context) ([]ScrapedSet, error) {
	seen := map[string]ScrapedSet{}

	for _, cat := range zeonMarketSources {
		for page := 1; ; page++ {
			if page > zeonMarketMaxPages {
				return nil, fmt.Errorf("category %s: still returning products after %d pages — pagination may have changed", cat.path, zeonMarketMaxPages)
			}
			body, err := z.fetcher.get(ctx, fmt.Sprintf("%s%s?page=%d", z.host, cat.path, page))
			if err != nil {
				return nil, fmt.Errorf("category %s page %d: %w", cat.path, page, err)
			}
			cards := zeonCardSplit.Split(string(body), -1)[1:]
			if len(cards) == 0 {
				if page == 1 {
					return nil, fmt.Errorf("category %s: no products on first page — site structure may have changed", cat.path)
				}
				break
			}
			for _, card := range cards {
				set, err := parseZeonCard(card, cat.grade, cat.preorder)
				if err != nil {
					return nil, fmt.Errorf("category %s page %d: %w", cat.path, page, err)
				}
				seen[set.ExternalID] = set // last category to see a product wins
			}
		}
	}

	sets := make([]ScrapedSet, 0, len(seen))
	for _, s := range seen {
		sets = append(sets, s)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].ExternalID < sets[j].ExternalID })
	return sets, nil
}

func parseZeonCard(card, grade string, preorder bool) (ScrapedSet, error) {
	title := zeonTitleRe.FindStringSubmatch(card)
	price := zeonPriceRe.FindStringSubmatch(card)
	if title == nil || price == nil {
		return ScrapedSet{}, fmt.Errorf("unrecognised product card layout — site structure may have changed")
	}
	productURL := html.UnescapeString(title[1])
	// Out-of-stock cards have no add-to-cart button, hence no
	// data-product-id, so the URL path is the only identifier present on
	// every card. Using it for all keeps IDs stable across stock changes.
	extID := strings.TrimPrefix(productURL, zeonMarketHost)
	name := html.UnescapeString(strings.TrimSpace(title[2]))
	// Price text looks like "€ 94,95 *" (asterisk = incl. VAT footnote).
	cents, err := ParseEuroPriceString(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(html.UnescapeString(price[1])), "*")))
	if err != nil {
		return ScrapedSet{}, fmt.Errorf("product %s (%s): %w", extID, name, err)
	}

	set := ScrapedSet{
		ExternalID: extID,
		URL:        productURL,
		Name:       name,
		Grade:      grade,
		PriceCents: cents,
		Currency:   zeonMarketCurrency,
	}
	if preorder {
		set.Availability = AvailabilityPreorder
		set.Name = zeonPreorderPrefix.ReplaceAllString(set.Name, "")
		set.Grade, _ = classifyGrade(set.Name)
		return set, nil
	}
	// "Op voorraad" / "Beperkt op voorraad" (limited) are buyable;
	// "Niet op voorraad" is not. Unknown markup leaves it unknown.
	if m := zeonStockRe.FindStringSubmatch(card); m != nil {
		set.Availability = AvailabilityFromBool(m[1] != "error")
	}
	return set, nil
}
