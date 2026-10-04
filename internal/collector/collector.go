// Package collector orchestrates a single scrape run: fetch, upsert sets,
// record price history, and deactivate sets no longer listed.
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gunpla-collector/internal/scraper"
	"gunpla-collector/internal/store"
)

// minSetsRatio guards against a broken scraper (e.g. a changed selector)
// silently wiping out the catalog: if a run returns fewer than this
// fraction of the previous successful run's set count, it's treated as a
// failure instead of a mass removal.
const minSetsRatio = 0.5

const (
	// eanRecheckAfter is how long a lookup that found no barcode is
	// trusted before the set is looked up again; shops add them later.
	eanRecheckAfter = 30 * 24 * time.Hour
	// maxEANLookupFailures in a row ends this run's lookups: the shop is
	// likely down or blocking, and each failure already waited out retries.
	maxEANLookupFailures = 3
)

// Options controls one collect run's behavior beyond the shop/scraper it
// runs against.
type Options struct {
	// Force skips the sanity guard (minSetsRatio). Use when a shop has
	// genuinely shrunk its catalog and the guard would otherwise block
	// every run forever.
	Force bool
}

type Store interface {
	StartRun(ctx context.Context, shopID int64, startedAt string) (int64, error)
	FinishRunFailed(ctx context.Context, runID int64, finishedAt string, errMsg string) error
	LastSuccessfulRunSetsFound(ctx context.Context, shopID int64) (int, bool, error)
	// ApplyRun persists a fetched set of results as a single atomic
	// operation: upsert sets, record price history, deactivate sets no
	// longer seen, and mark the run successful.
	ApplyRun(ctx context.Context, shopID, runID int64, sets []scraper.ScrapedSet, now string) error
	// EANChecks and MarkEANChecked back barcode lookups for scrapers
	// implementing scraper.BarcodeLookup.
	EANChecks(ctx context.Context, shopID int64) (map[string]store.EANCheck, error)
	MarkEANChecked(ctx context.Context, shopID int64, externalIDs []string, now string) error
}

func Run(ctx context.Context, db Store, shop store.Shop, s scraper.Scraper, logger *slog.Logger, opts Options) error {
	now := func() string { return time.Now().UTC().Format(time.RFC3339) }

	startedAt := now()
	runID, err := db.StartRun(ctx, shop.ID, startedAt)
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	sets, fetchErr := s.FetchAll(ctx)
	if fetchErr != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), fetchErr.Error())
		return fmt.Errorf("fetch %s: %w", shop.Slug, fetchErr)
	}

	// Accessories, figures and other product lines that shops file under a
	// grade category aren't Gunpla kits; don't track them.
	sets, dropped := scraper.FilterKits(sets)
	if len(dropped) > 0 {
		logger.Info("skipped non-kit listings", "shop", shop.Slug, "count", len(dropped))
		logger.Debug("non-kit listings", "shop", shop.Slug, "names", dropped)
	}

	prevCount, ok, err := db.LastSuccessfulRunSetsFound(ctx, shop.ID)
	if err != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), err.Error())
		return fmt.Errorf("check previous run count for %s: %w", shop.Slug, err)
	}
	if ok && float64(len(sets)) < float64(prevCount)*minSetsRatio {
		if !opts.Force {
			msg := fmt.Sprintf("sanity guard: fetched %d sets, previous successful run had %d (below %.0f%% threshold) — refusing to treat as mass removal; rerun with --force to override",
				len(sets), prevCount, minSetsRatio*100)
			_ = db.FinishRunFailed(ctx, runID, now(), msg)
			return fmt.Errorf("%s", msg)
		}
		logger.Warn("sanity guard tripped but --force set — proceeding anyway",
			"shop", shop.Slug, "sets_found", len(sets), "previous_sets_found", prevCount)
	}

	var eanChecked []string
	if l, ok := s.(scraper.BarcodeLookup); ok {
		eanChecked = lookupEANs(ctx, db, shop, l, sets, logger)
	}

	if err := db.ApplyRun(ctx, shop.ID, runID, sets, now()); err != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), err.Error())
		return fmt.Errorf("apply run for %s: %w", shop.Slug, err)
	}

	// After ApplyRun, so sets first seen in this run exist to be marked.
	// Losing this only means those sets are looked up again next run.
	if len(eanChecked) > 0 {
		if err := db.MarkEANChecked(ctx, shop.ID, eanChecked, now()); err != nil {
			logger.Warn("record barcode lookups failed", "shop", shop.Slug, "err", err)
		}
	}

	logger.Info("collect run complete", "shop", shop.Slug, "sets_found", len(sets), "run_id", runID)
	return nil
}

// lookupEANs fills in barcodes the listing left out: the stored one for a
// set that has one, a fresh lookup for a set never looked up or last
// looked up over eanRecheckAfter ago. It returns the external ids it
// looked up, found or not. Failures are logged, never fatal: a
// missing barcode only weakens cross-shop matching.
func lookupEANs(ctx context.Context, db Store, shop store.Shop, l scraper.BarcodeLookup, sets []scraper.ScrapedSet, logger *slog.Logger) []string {
	known, err := db.EANChecks(ctx, shop.ID)
	if err != nil {
		logger.Warn("skipping barcode lookups", "shop", shop.Slug, "err", err)
		return nil
	}
	recheckBefore := time.Now().UTC().Add(-eanRecheckAfter).Format(time.RFC3339)

	var checked []string
	found, failed, failures := 0, 0, 0 // failures: in a row
	for i := range sets {
		sc := &sets[i]
		if sc.EAN != "" {
			continue
		}
		k := known[sc.ExternalID]
		if k.EAN != "" {
			sc.EAN = k.EAN // keep it: ApplyRun overwrites the stored EAN
			continue
		}
		if k.CheckedAt != "" && k.CheckedAt >= recheckBefore {
			continue
		}
		if failures >= maxEANLookupFailures || ctx.Err() != nil {
			continue // keep going: later sets may still have a stored EAN to fill in
		}
		ean, err := l.LookupEAN(ctx, *sc)
		if err != nil {
			failed++
			failures++
			logger.Warn("barcode lookup failed", "shop", shop.Slug, "set", sc.ExternalID, "err", err)
			continue
		}
		failures = 0
		checked = append(checked, sc.ExternalID)
		if ean != "" {
			sc.EAN = ean
			found++
		}
	}
	if len(checked) > 0 || failed > 0 {
		logger.Info("barcode lookups", "shop", shop.Slug, "looked_up", len(checked), "found", found, "failed", failed)
	}
	return checked
}
