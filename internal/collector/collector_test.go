package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fakeStore: minimal in-memory implementation of the Store interface ---

type runRecord struct {
	shopID    int64
	status    string
	setsFound int
	err       string
}

type priceRecord struct {
	setID      int64
	runID      int64
	priceCents int
	inStock    *bool
}

type fakeStore struct {
	nextRunID       int64
	nextSetID       int64
	runs            map[int64]*runRecord
	setIDByExtID    map[string]int64
	priceHistory    []priceRecord
	deactivateCalls [][]string
	upsertCalls     int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		runs:         map[int64]*runRecord{},
		setIDByExtID: map[string]int64{},
	}
}

func (f *fakeStore) StartRun(ctx context.Context, shopID int64, startedAt string) (int64, error) {
	f.nextRunID++
	f.runs[f.nextRunID] = &runRecord{shopID: shopID, status: "running"}
	return f.nextRunID, nil
}

func (f *fakeStore) FinishRunFailed(ctx context.Context, runID int64, finishedAt string, errMsg string) error {
	f.runs[runID].status = "failed"
	f.runs[runID].err = errMsg
	return nil
}

func (f *fakeStore) LastSuccessfulRunSetsFound(ctx context.Context, shopID int64) (int, bool, error) {
	var best *runRecord
	var bestID int64
	for id, r := range f.runs {
		if r.shopID == shopID && r.status == "success" {
			if best == nil || id > bestID {
				best = r
				bestID = id
			}
		}
	}
	if best == nil {
		return 0, false, nil
	}
	return best.setsFound, true, nil
}

// ApplyRun mirrors store.Store.ApplyRun's observable behavior (upsert +
// price history + deactivate + finish-success) without real transactions —
// fine for a fake, since these tests only assert on the end state.
func (f *fakeStore) ApplyRun(ctx context.Context, shopID, runID int64, sets []scraper.ScrapedSet, now string) error {
	seenIDs := make([]string, 0, len(sets))
	for _, sc := range sets {
		f.upsertCalls++
		id, ok := f.setIDByExtID[sc.ExternalID]
		if !ok {
			f.nextSetID++
			id = f.nextSetID
			f.setIDByExtID[sc.ExternalID] = id
		}
		f.priceHistory = append(f.priceHistory, priceRecord{setID: id, runID: runID, priceCents: sc.PriceCents, inStock: sc.InStock})
		seenIDs = append(seenIDs, sc.ExternalID)
	}
	f.deactivateCalls = append(f.deactivateCalls, seenIDs)
	f.runs[runID].status = "success"
	f.runs[runID].setsFound = len(sets)
	return nil
}

// --- fakeScraper ---

type fakeScraper struct {
	sets []scraper.ScrapedSet
	err  error
}

func (f *fakeScraper) ShopSlug() string { return "fake" }
func (f *fakeScraper) ShopName() string { return "Fake" }
func (f *fakeScraper) BaseURL() string  { return "https://example.com" }
func (f *fakeScraper) FetchAll(ctx context.Context) ([]scraper.ScrapedSet, error) {
	return f.sets, f.err
}

func mkSets(n int) []scraper.ScrapedSet {
	sets := make([]scraper.ScrapedSet, n)
	for i := range sets {
		sets[i] = scraper.ScrapedSet{
			ExternalID: fmt.Sprintf("id-%d", i),
			URL:        fmt.Sprintf("https://example.com/%d.html", i),
			Name:       fmt.Sprintf("Set %d", i),
			Grade:      "MG",
			PriceCents: 1000 + i,
			Currency:   "EUR",
		}
	}
	return sets
}

func TestRun_Success(t *testing.T) {
	db := newFakeStore()
	shop := store.Shop{ID: 1, Slug: "fake"}
	sc := &fakeScraper{sets: mkSets(3)}

	if err := Run(context.Background(), db, shop, sc, discardLogger(), Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(db.priceHistory) != 3 {
		t.Errorf("got %d price_history rows, want 3", len(db.priceHistory))
	}
	if len(db.deactivateCalls) != 1 || len(db.deactivateCalls[0]) != 3 {
		t.Errorf("DeactivateMissing called with %v, want one call with 3 ids", db.deactivateCalls)
	}
	r := db.runs[1]
	if r.status != "success" || r.setsFound != 3 {
		t.Errorf("run record = %+v, want status=success setsFound=3", r)
	}
}

func TestRun_FetchErrorDoesNotTouchSets(t *testing.T) {
	db := newFakeStore()
	shop := store.Shop{ID: 1, Slug: "fake"}
	sc := &fakeScraper{err: errors.New("boom")}

	if err := Run(context.Background(), db, shop, sc, discardLogger(), Options{}); err == nil {
		t.Fatal("expected error, got nil")
	}

	if db.upsertCalls != 0 {
		t.Errorf("UpsertSet called %d times, want 0", db.upsertCalls)
	}
	if len(db.deactivateCalls) != 0 {
		t.Errorf("DeactivateMissing called %d times, want 0", len(db.deactivateCalls))
	}
	r := db.runs[1]
	if r.status != "failed" || r.err == "" {
		t.Errorf("run record = %+v, want status=failed with error message", r)
	}
}

// TestRun_SanityGuardPreventsMassRemoval verifies the guard (minSetsRatio):
// a run that returns far fewer sets than the previous successful run is
// treated as a failure, and must not touch sets/mark anything removed
// (i.e. DeactivateMissing is never called).
func TestRun_SanityGuardPreventsMassRemoval(t *testing.T) {
	db := newFakeStore()
	shop := store.Shop{ID: 1, Slug: "fake"}

	// Seed a healthy baseline run with 100 sets.
	baseline := &fakeScraper{sets: mkSets(100)}
	if err := Run(context.Background(), db, shop, baseline, discardLogger(), Options{}); err != nil {
		t.Fatalf("baseline Run: %v", err)
	}
	if len(db.deactivateCalls) != 1 {
		t.Fatalf("expected 1 deactivate call after baseline, got %d", len(db.deactivateCalls))
	}

	// A broken scraper suddenly returns only 10 sets (< 50% of 100).
	broken := &fakeScraper{sets: mkSets(10)}
	err := Run(context.Background(), db, shop, broken, discardLogger(), Options{})
	if err == nil {
		t.Fatal("expected sanity guard error, got nil")
	}

	// Crucially: no additional DeactivateMissing call happened, so no set
	// from the baseline run was wrongly marked removed.
	if len(db.deactivateCalls) != 1 {
		t.Errorf("DeactivateMissing called %d times total, want still 1 (guard must short-circuit before step 3-4)", len(db.deactivateCalls))
	}
	r := db.runs[2]
	if r.status != "failed" {
		t.Errorf("second run status = %q, want failed", r.status)
	}
}

// TestRun_ForceBypassesSanityGuard verifies Options{Force: true} lets a run
// through that the guard would otherwise refuse — the escape hatch for a
// shop that has genuinely shrunk its catalog.
func TestRun_ForceBypassesSanityGuard(t *testing.T) {
	db := newFakeStore()
	shop := store.Shop{ID: 1, Slug: "fake"}

	baseline := &fakeScraper{sets: mkSets(100)}
	if err := Run(context.Background(), db, shop, baseline, discardLogger(), Options{}); err != nil {
		t.Fatalf("baseline Run: %v", err)
	}

	broken := &fakeScraper{sets: mkSets(10)}
	if err := Run(context.Background(), db, shop, broken, discardLogger(), Options{Force: true}); err != nil {
		t.Fatalf("forced Run: %v", err)
	}

	if len(db.deactivateCalls) != 2 {
		t.Errorf("DeactivateMissing called %d times, want 2 (guard bypassed, so this run applied normally)", len(db.deactivateCalls))
	}
	r := db.runs[2]
	if r.status != "success" || r.setsFound != 10 {
		t.Errorf("forced run record = %+v, want status=success setsFound=10", r)
	}
}
