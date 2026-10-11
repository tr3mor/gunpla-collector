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
	// eanFlushEvery is how many lookups are held in memory before they're
	// saved, so a run that dies partway through a long backfill loses at
	// most this many.
	eanFlushEvery = 25
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
	// EANChecks and RecordEANLookups back barcode lookups for scrapers
	// implementing scraper.BarcodeLookup.
	EANChecks(ctx context.Context, shopID int64) (map[string]store.EANCheck, error)
	RecordEANLookups(ctx context.Context, shopID int64, lookups []store.EANLookup, now string) error
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

	if err := db.ApplyRun(ctx, shop.ID, runID, sets, now()); err != nil {
		_ = db.FinishRunFailed(ctx, runID, now(), err.Error())
		return fmt.Errorf("apply run for %s: %w", shop.Slug, err)
	}

	logger.Info("collect run complete", "shop", shop.Slug, "sets_found", len(sets), "run_id", runID)

	// After ApplyRun, so the run is already saved if a long backfill is cut
	// short, and sets first seen in this run exist to record barcodes on.
	// Failures here never fail the run: a missing barcode only weakens
	// cross-shop matching.
	if l, ok := s.(scraper.BarcodeLookup); ok {
		lookupEANs(ctx, db, shop, l, sets, logger)
	}
	return nil
}

// lookupEANs fills in barcodes the listing left out: every set with no
// barcode that was never looked up, or last looked up over
// eanRecheckAfter ago, is looked up and the result saved in batches. A
// run cut short (timeout, signal, crash) keeps what it saved, and the next
// run carries on with the rest. Failures are logged, never fatal.
func lookupEANs(ctx context.Context, db Store, shop store.Shop, l scraper.BarcodeLookup, sets []scraper.ScrapedSet, logger *slog.Logger) {
	known, err := db.EANChecks(ctx, shop.ID)
	if err != nil {
		logger.Warn("skipping barcode lookups", "shop", shop.Slug, "err", err)
		return
	}
	recheckBefore := time.Now().UTC().Add(-eanRecheckAfter).Format(time.RFC3339)

	var pending []store.EANLookup
	flush := func() {
		if len(pending) == 0 {
			return
		}
		// Detached from ctx: a cancelled run should still save what it has.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := db.RecordEANLookups(saveCtx, shop.ID, pending, time.Now().UTC().Format(time.RFC3339)); err != nil {
			logger.Warn("record barcode lookups failed, will retry", "shop", shop.Slug, "err", err)
			return // keep pending for the next flush
		}
		pending = pending[:0]
	}

	looked, found, failed, failures := 0, 0, 0, 0 // failures: in a row
	for i := range sets {
		sc := sets[i]
		k := known[sc.ExternalID] // includes a barcode the listing itself carried
		if k.EAN != "" {
			continue
		}
		if k.CheckedAt != "" && k.CheckedAt >= recheckBefore {
			continue
		}
		if failures >= maxEANLookupFailures || ctx.Err() != nil {
			break
		}
		ean, err := l.LookupEAN(ctx, sc)
		if err != nil {
			failed++
			failures++
			logger.Warn("barcode lookup failed", "shop", shop.Slug, "set", sc.ExternalID, "err", err)
			continue
		}
		failures = 0
		looked++
		if ean != "" {
			found++
		}
		pending = append(pending, store.EANLookup{ExternalID: sc.ExternalID, EAN: ean})
		if len(pending) >= eanFlushEvery {
			flush()
		}
	}
	flush()
	if looked > 0 || failed > 0 {
		logger.Info("barcode lookups", "shop", shop.Slug, "looked_up", looked, "found", found, "failed", failed)
	}
}
