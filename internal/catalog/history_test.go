package catalog

import (
	"context"
	"reflect"
	"testing"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// historyStore serves fixed histories; the other Store methods are unused.
type historyStore struct {
	Store
	hs []store.SetHistory
}

func (f *historyStore) History(ctx context.Context, productID, setID int64) ([]store.SetHistory, error) {
	return f.hs, nil
}

// runs numbers the shop's successful runs 1..n, one per day.
func runs(ids ...int64) []store.HistoryRun {
	var out []store.HistoryRun
	for _, id := range ids {
		out = append(out, store.HistoryRun{ID: id, StartedAt: day(id)})
	}
	return out
}

func day(run int64) string { return "2026-01-0" + string(rune('0'+run)) + "T18:00:00Z" }

func rec(run int64, eur int, av scraper.Availability) store.HistoryRecord {
	return store.HistoryRecord{RunID: run, ScrapedAt: day(run), PriceCents: eur, Currency: "EUR", EURCents: eur, HasEUR: true, Availability: av}
}

// shape reduces entries to "run:state" so a test reads as a timeline:
// P buyable point, O out-of-stock point, - not listed.
func shape(es []Entry) []string {
	var out []string
	for _, e := range es {
		switch {
		case !e.Listed:
			out = append(out, e.At[8:10]+"-")
		case e.Availability == scraper.AvailabilityOutOfStock:
			out = append(out, e.At[8:10]+"O")
		default:
			out = append(out, e.At[8:10]+"P")
		}
	}
	return out
}

func TestHistory_NotListedRuns(t *testing.T) {
	in := scraper.AvailabilityInStock
	cases := []struct {
		name string
		h    store.SetHistory
		want []string
	}{
		{"removed and relisted",
			store.SetHistory{Records: []store.HistoryRecord{rec(1, 4000, in), rec(2, 4000, in), rec(5, 4200, in)}, Runs: runs(1, 2, 3, 4, 5)},
			[]string{"01P", "02P", "03-", "04-", "05P"}},
		{"removed for good",
			store.SetHistory{Records: []store.HistoryRecord{rec(1, 4000, in), rec(2, 4000, in)}, Runs: runs(1, 2, 3, 4)},
			[]string{"01P", "02P", "03-", "04-"}},
		{"failed run in between leaves no gap",
			// run 3 failed, so the store does not list it among the shop's successful runs
			store.SetHistory{Records: []store.HistoryRecord{rec(2, 4000, in), rec(4, 4100, in)}, Runs: runs(2, 4)},
			[]string{"02P", "04P"}},
		{"runs before the first record are not shown",
			store.SetHistory{Records: []store.HistoryRecord{rec(3, 4000, in)}, Runs: runs(1, 2, 3)},
			[]string{"03P"}},
		{"out of stock keeps its price, unknown and preorder are buyable",
			store.SetHistory{Records: []store.HistoryRecord{
				rec(1, 4000, in), rec(2, 4000, scraper.AvailabilityOutOfStock),
				rec(3, 4000, scraper.AvailabilityUnknown), rec(4, 4000, scraper.AvailabilityPreorder)}, Runs: runs(1, 2, 3, 4)},
			[]string{"01P", "02O", "03P", "04P"}},
		{"unchanged price still gives a point per run",
			store.SetHistory{Records: []store.HistoryRecord{rec(1, 4000, in), rec(2, 4000, in), rec(3, 4000, in)}, Runs: runs(1, 2, 3)},
			[]string{"01P", "02P", "03P"}},
		{"no records",
			store.SetHistory{Runs: runs(1, 2)},
			nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(&historyStore{hs: []store.SetHistory{tc.h}})
			got, err := c.History(context.Background(), 0, 1)
			if err != nil {
				t.Fatal(err)
			}
			if s := shape(got[0].Entries); !reflect.DeepEqual(s, tc.want) {
				t.Errorf("entries = %v, want %v", s, tc.want)
			}
		})
	}
}

func TestHistory_RecordWithoutEURIsLeftOutAndIsNotAGap(t *testing.T) {
	noEUR := store.HistoryRecord{RunID: 2, ScrapedAt: day(2), PriceCents: 4200, Currency: "CHF"}
	h := store.SetHistory{
		Records: []store.HistoryRecord{rec(1, 4000, scraper.AvailabilityInStock), noEUR, rec(3, 4000, scraper.AvailabilityInStock)},
		Runs:    runs(1, 2, 3),
	}
	got, _ := New(&historyStore{hs: []store.SetHistory{h}}).History(context.Background(), 0, 1)
	if want := []string{"01P", "03P"}; !reflect.DeepEqual(shape(got[0].Entries), want) {
		t.Errorf("entries = %v, want %v", shape(got[0].Entries), want)
	}
}

func TestHistory_KeepsShopPriceBesideEUR(t *testing.T) {
	r := store.HistoryRecord{RunID: 1, ScrapedAt: day(1), PriceCents: 4200, Currency: "USD", EURCents: 3800, HasEUR: true}
	h := store.SetHistory{ShopName: "Gundam Store", Records: []store.HistoryRecord{r}, Runs: runs(1)}
	got, _ := New(&historyStore{hs: []store.SetHistory{h}}).History(context.Background(), 0, 1)
	e := got[0].Entries[0]
	if e.EURCents != 3800 || e.Cents != 4200 || e.Currency != "USD" {
		t.Errorf("entry = %+v, want 3800 EUR beside 4200 USD", e)
	}
}

func TestHistory_TwoListingsOfOneShopStaySeparate(t *testing.T) {
	in := scraper.AvailabilityInStock
	hs := []store.SetHistory{
		{SetID: 1, ShopName: "Shop A", Name: "Kit", Active: true, Records: []store.HistoryRecord{rec(1, 4000, in)}, Runs: runs(1)},
		{SetID: 2, ShopName: "Shop A", Name: "Kit (re-release)", Active: false, Records: []store.HistoryRecord{rec(1, 4500, in)}, Runs: runs(1)},
	}
	got, _ := New(&historyStore{hs: hs}).History(context.Background(), 7, 0)
	if len(got) != 2 || got[0].SetID != 1 || got[1].SetID != 2 || got[1].Active {
		t.Fatalf("series = %+v, want two separate series in store order", got)
	}
}
