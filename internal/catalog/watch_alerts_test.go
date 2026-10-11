package catalog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf8"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// shopFixture drives a real store through successive scrapes of shops.
type shopFixture struct {
	t     *testing.T
	s     *store.Store
	runs  int
	shops map[string]store.Shop
}

func newShopFixture(t *testing.T) *shopFixture {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &shopFixture{t: t, s: s, shops: map[string]store.Shop{}}
}

// scrape records a full catalog for a shop; sets missing from it become inactive.
func (f *shopFixture) scrape(slug string, sets ...scraper.ScrapedSet) {
	f.t.Helper()
	ctx := context.Background()
	shop, ok := f.shops[slug]
	if !ok {
		var err error
		if shop, err = f.s.GetOrCreateShop(ctx, slug, slug, "https://"+slug); err != nil {
			f.t.Fatal(err)
		}
		f.shops[slug] = shop
	}
	f.runs++
	at := fmt.Sprintf("2026-01-01T00:%02d:00Z", f.runs)
	runID, err := f.s.StartRun(ctx, shop.ID, at)
	if err != nil {
		f.t.Fatal(err)
	}
	for i := range sets {
		if sets[i].URL == "" {
			sets[i].URL = "https://" + slug + "/" + sets[i].ExternalID
		}
		if sets[i].Currency == "" {
			sets[i].Currency = "EUR"
		}
	}
	if err := f.s.ApplyRun(ctx, shop.ID, runID, sets, at); err != nil {
		f.t.Fatal(err)
	}
	if err := f.s.FinishRunSuccess(ctx, runID, at, len(sets)); err != nil {
		f.t.Fatal(err)
	}
}

func set(id, name string, cents int, avail scraper.Availability) scraper.ScrapedSet {
	return scraper.ScrapedSet{ExternalID: id, Name: name, Grade: "MG", PriceCents: cents, Availability: avail}
}

const (
	inStock = scraper.AvailabilityInStock
	oos     = scraper.AvailabilityOutOfStock
)

func reasons(p *WatchPass) [][]string {
	var out [][]string
	for _, a := range p.Alerts {
		out = append(out, a.Reasons)
	}
	return out
}

// pass evaluates and commits, returning the alerts raised.
func pass(t *testing.T, c *Catalog) *WatchPass {
	t.Helper()
	p, err := c.EvaluateWatches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return p
}

func kaWatch(max int, restock, newL bool) Watch {
	return Watch{Label: "FAZZ Ver.Ka", Enabled: true, MaxPriceCents: max, OnRestock: restock, OnNew: newL,
		Spec: WatchSpec{Grade: "MG", Model: "fa010s", Tokens: []string{"armor", "full", "zz"}, Variants: []string{"verka"}}}
}

func TestEvaluateWatches_TargetAlertsOnceAndRearms(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	ctx := context.Background()
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, inStock))
	if _, err := c.SaveWatch(ctx, kaWatch(12000, false, false)); err != nil {
		t.Fatal(err)
	}

	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("above target: alerts %v, want none", got)
	}
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 11000, inStock))
	if got := reasons(pass(t, c)); !reflect.DeepEqual(got, [][]string{{ReasonTarget}}) {
		t.Fatalf("under target: alerts %v, want one target alert", got)
	}
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("repeat pass: alerts %v, want none", got)
	}
	// A small wobble under the target is not news.
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 10900, inStock))
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("1%% drop: alerts %v, want none", got)
	}
	// A real further drop is.
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 9000, inStock))
	if got := reasons(pass(t, c)); len(got) != 1 {
		t.Fatalf("further drop: alerts %v, want one", got)
	}
	// Leaving the target and coming back alerts again.
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 14000, inStock))
	pass(t, c)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 11500, inStock))
	if got := reasons(pass(t, c)); len(got) != 1 {
		t.Fatalf("re-entering target: alerts %v, want one", got)
	}
}

func TestEvaluateWatches_AlreadyUnderTargetAlertsOnFirstPass(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 9000, inStock))
	if _, err := c.SaveWatch(context.Background(), kaWatch(12000, true, true)); err != nil {
		t.Fatal(err)
	}
	p := pass(t, c)
	if got := reasons(p); !reflect.DeepEqual(got, [][]string{{ReasonTarget}}) {
		t.Fatalf("alerts %v, want only the target (restock/new need a baseline first)", got)
	}
	if !p.Alerts[0].HasBest || p.Alerts[0].Best.EURCents != 9000 {
		t.Errorf("best = %+v", p.Alerts[0].Best)
	}
}

func TestEvaluateWatches_RestockAndNewListing(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, oos))
	if _, err := c.SaveWatch(context.Background(), kaWatch(0, true, true)); err != nil {
		t.Fatal(err)
	}
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("baseline: alerts %v, want none", got)
	}
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, inStock))
	if got := reasons(pass(t, c)); !reflect.DeepEqual(got, [][]string{{ReasonRestock}}) {
		t.Fatalf("restock: alerts %v", got)
	}
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("restock repeated: %v", got)
	}
	// Another shop starts selling it: a new listing, and nothing else.
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, inStock))
	f.scrape("b", set("9", "MG – FA-010S Full Armor ZZ Gundam Ver.Ka", 14000, inStock))
	p := pass(t, c)
	if got := reasons(p); !reflect.DeepEqual(got, [][]string{{ReasonNew}}) || len(p.Alerts[0].NewListings) != 1 {
		t.Fatalf("new listing: alerts %v", got)
	}
	// Unrelated kits never trigger anything.
	f.scrape("b", set("9", "MG – FA-010S Full Armor ZZ Gundam Ver.Ka", 14000, inStock), set("10", "MG Zaku II", 3000, inStock))
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("unrelated listing: alerts %v", got)
	}
}

func TestEvaluateWatches_UncommittedAlertsRepeat(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 9000, inStock))
	if _, err := c.SaveWatch(context.Background(), kaWatch(12000, false, false)); err != nil {
		t.Fatal(err)
	}
	// A failed send means Commit is never called: the alert must come back.
	for i := 0; i < 2; i++ {
		p, err := c.EvaluateWatches(context.Background())
		if err != nil || len(p.Alerts) != 1 {
			t.Fatalf("pass %d: %v alerts, err %v; want 1", i, len(p.Alerts), err)
		}
	}
}

func TestEvaluateWatches_DisabledAndEditedWatches(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	ctx := context.Background()
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 9000, inStock))
	w := kaWatch(12000, false, false)
	w.Enabled = false
	saved, err := c.SaveWatch(ctx, w)
	if err != nil {
		t.Fatal(err)
	}
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("disabled watch alerted: %v", got)
	}
	saved.Enabled = true
	if _, err := c.SaveWatch(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if got := reasons(pass(t, c)); len(got) != 1 {
		t.Fatalf("enabled: %v, want one alert", got)
	}
	// Renaming keeps the alert state; changing the target restarts it.
	saved.Label = "renamed"
	if _, err := c.SaveWatch(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("after rename: %v, want none", got)
	}
	saved.MaxPriceCents = 13000
	if _, err := c.SaveWatch(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if got := reasons(pass(t, c)); len(got) != 1 {
		t.Fatalf("after retarget: %v, want a fresh alert", got)
	}
}

func TestEvaluateWatches_ConvertsForeignPricesAndIgnoresUnpriced(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s) // no rates: USD can't be compared
	f.scrape("a", scraper.ScrapedSet{ExternalID: "1", Name: "MG Full Armor ZZ Gundam Ver.Ka", Grade: "MG", PriceCents: 100, Currency: "USD", Availability: inStock})
	if _, err := c.SaveWatch(context.Background(), kaWatch(12000, false, false)); err != nil {
		t.Fatal(err)
	}
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("unconvertible price alerted: %v", got)
	}
}

func TestSaveWatch_Validation(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	ctx := context.Background()
	cases := map[string]func(w *Watch){
		"no name":          func(w *Watch) { w.Label = "  " },
		"negative target":  func(w *Watch) { w.MaxPriceCents = -1 },
		"absurd target":    func(w *Watch) { w.MaxPriceCents = maxPriceCents + 1 },
		"no alert enabled": func(w *Watch) { w.MaxPriceCents, w.OnRestock, w.OnNew = 0, false, false },
		"matches nothing":  func(w *Watch) { w.Spec = WatchSpec{Grade: "MG"} },
	}
	for name, mutate := range cases {
		w := kaWatch(12000, true, false)
		mutate(&w)
		_, err := c.SaveWatch(ctx, w)
		var bad *ErrInvalidWatch
		if !errors.As(err, &bad) {
			t.Errorf("%s: err = %v, want ErrInvalidWatch", name, err)
		}
	}
	if _, err := c.SaveWatch(ctx, Watch{ID: 999, Label: "x", Enabled: true, OnRestock: true, Spec: WatchSpec{Model: "x1"}}); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("update of a missing watch: err = %v, want ErrWatchNotFound", err)
	}
}

func TestPreviewWatch_PinsVariantsAndMissingVersions(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	ctx := context.Background()
	f.scrape("a",
		set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, inStock),
		set("2", "MG FA-010S Full Armor ZZ Gundam 1/100", 9000, inStock),
		set("3", "MG Wing Gundam Zero", 8000, inStock),
	)
	f.scrape("b", scraper.ScrapedSet{ExternalID: "x", Name: "Gundam FAZZ Pre-order", PriceCents: 12000, Availability: scraper.AvailabilityPreorder}) // no grade

	spec := WatchSpec{Grade: "MG", Model: "fa010s", Tokens: []string{"armor", "full", "zz"}, Variants: []string{"verka", "ver9"}}
	p, err := c.PreviewWatch(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Matches) != 1 || p.Matches[0].Variant != "verka" {
		t.Fatalf("matches = %+v, want only the Ver.Ka listing", p.Matches)
	}
	if len(p.Variants) != 2 {
		t.Errorf("variants = %+v, want Ver.Ka and the base release", p.Variants)
	}
	if !reflect.DeepEqual(p.MissingVariants, []string{"ver9"}) {
		t.Errorf("missing = %v, want [ver9]", p.MissingVariants)
	}

	// Pin the ungraded pre-order from the other shop, and exclude the match.
	rows, _ := f.s.SearchSets(ctx, "b")
	spec.Include = []int64{rows[0].SetID}
	spec.Exclude = []int64{p.Matches[0].SetID}
	p, err = c.PreviewWatch(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Matches) != 2 || !p.Matches[0].Pinned || p.Matches[0].Excluded || !p.Matches[1].Excluded {
		t.Fatalf("matches = %+v, want the pinned one live and the excluded one last", p.Matches)
	}
	if best, ok := p.Best(); !ok || best.SetID != rows[0].SetID {
		t.Errorf("best = %+v, want the pinned pre-order (the excluded one never wins)", best)
	}
	if len(p.Variants) != 2 {
		t.Errorf("a pinned listing of another suit must not add variants: %+v", p.Variants)
	}
}

func TestSpecFromSet_PinsANameThatCannotMatchItself(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	ctx := context.Background()
	f.scrape("a", set("1", "MG Gundam", 5000, inStock)) // no model, no distinctive word
	rows, _ := f.s.SearchSets(ctx, "")
	spec, _, err := c.SpecFromSet(ctx, rows[0].SetID)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := c.PreviewWatch(ctx, spec)
	if len(p.Matches) != 1 {
		t.Fatalf("the starred listing must be in its own watch, got %+v", p.Matches)
	}
	if _, _, err := c.SpecFromSet(ctx, 12345); err == nil {
		t.Error("unknown set: want an error")
	}
}

func TestEvaluateWatches_AListingThatFlapsIsNotNewAgain(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, inStock))
	if _, err := c.SaveWatch(context.Background(), kaWatch(0, false, true)); err != nil {
		t.Fatal(err)
	}
	pass(t, c)
	f.scrape("a") // the shop's catalog briefly lacks it
	pass(t, c)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 15000, inStock))
	if got := reasons(pass(t, c)); len(got) != 0 {
		t.Fatalf("returning listing alerted as new: %v", got)
	}
}

func TestEvaluateWatches_RestockUnderTargetIsOneAlertReason(t *testing.T) {
	f := newShopFixture(t)
	c := New(f.s)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 9000, oos))
	if _, err := c.SaveWatch(context.Background(), kaWatch(12000, true, false)); err != nil {
		t.Fatal(err)
	}
	pass(t, c)
	f.scrape("a", set("1", "MG Full Armor ZZ Gundam Ver.Ka", 9000, inStock))
	if got := reasons(pass(t, c)); !reflect.DeepEqual(got, [][]string{{ReasonTarget}}) {
		t.Fatalf("alerts %v, want a single target reason", got)
	}
}

func TestSaveWatch_LongMultibyteWordIsRejectedCleanly(t *testing.T) {
	c := New(newShopFixture(t).s)
	w := kaWatch(12000, true, false)
	w.Spec.Tokens = []string{repeat("ν", 40)} // 80 bytes
	_, err := c.SaveWatch(context.Background(), w)
	var bad *ErrInvalidWatch
	if !errors.As(err, &bad) || !utf8.ValidString(err.Error()) {
		t.Fatalf("err = %v, want a valid-UTF-8 ErrInvalidWatch", err)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
